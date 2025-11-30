"""
HomeGuard Agentic AI Service

This service provides an AI-powered assistant for the HomeGuard IoT platform.
It uses Google Gemini for natural language understanding and tool execution.

OPTIMIZED: Minimizes Gemini API calls by:
1. Single call to plan all tool executions
2. Local tool execution without AI calls
3. Local response generation for common patterns
"""

import os
import json
import logging
import httpx
import asyncio
from datetime import datetime
from typing import Optional, Dict, Any, List
from uuid import uuid4

from fastapi import FastAPI, HTTPException, Header, BackgroundTasks
from fastapi.responses import StreamingResponse
from pydantic import BaseModel
from prometheus_client import Counter, Histogram, generate_latest, CONTENT_TYPE_LATEST
from starlette.responses import Response

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Environment configuration
GEMINI_TEXT_API_KEY = os.getenv("GEMINI_TEXT_API_KEY", "")
GEMINI_TEXT_MODEL = os.getenv("GEMINI_TEXT_MODEL", "gemini-pro-latest")
GEMINI_BASE_URL = "https://generativelanguage.googleapis.com/v1beta"

DEVICE_SERVICE_URL = os.getenv("DEVICE_SERVICE_URL", "http://iot-device-service.sandbox:8080")
NOTIFICATION_SERVICE_URL = os.getenv("NOTIFICATION_SERVICE_URL", "http://iot-notification-service.sandbox:8080")
N8N_WEBHOOK_BASE = os.getenv("N8N_WEBHOOK_BASE", "http://iot-n8n.sandbox:5678/webhook")

# Metrics
chat_requests = Counter("agentic_ai_chat_requests_total", "Total chat requests", ["status"])
chat_latency = Histogram("agentic_ai_chat_latency_seconds", "Chat request latency")
tool_calls = Counter("agentic_ai_tool_calls_total", "Tool calls made", ["tool_name"])
gemini_calls = Counter("agentic_ai_gemini_calls_total", "Gemini API calls", ["type"])

app = FastAPI(
    title="HomeGuard Agentic AI Service",
    description="AI-powered assistant for smart home management",
    version="1.0.0"
)


class ChatRequest(BaseModel):
    message: str
    conversation_id: Optional[str] = None
    context: Optional[Dict[str, Any]] = None


class ChatResponse(BaseModel):
    id: str
    conversation_id: str
    message: str
    actions_taken: List[Dict[str, Any]] = []
    suggestions: List[str] = []
    timestamp: str
    error: Optional[str] = None


class ActionRequest(BaseModel):
    action: str
    parameters: Dict[str, Any] = {}


# In-memory conversation storage (would use MongoDB in production)
conversations: Dict[str, List[Dict]] = {}


# Tool definitions for Gemini function calling
TOOLS = [
    {
        "name": "list_devices",
        "description": "Get a list of all IoT devices for the current user. Call this first to get device IDs.",
        "parameters": {
            "type": "object",
            "properties": {
                "device_type": {
                    "type": "string",
                    "description": "Optional filter by device type (e.g., 'light', 'thermostat', 'camera', 'smart_lock')"
                }
            }
        }
    },
    {
        "name": "get_device_status",
        "description": "Get the current status of a specific device including on/off state, temperature, etc.",
        "parameters": {
            "type": "object",
            "properties": {
                "device_id": {
                    "type": "string",
                    "description": "The ID of the device to check"
                }
            },
            "required": ["device_id"]
        }
    },
    {
        "name": "send_device_command",
        "description": "Control a device. Commands: turn_on, turn_off, set_temperature, set_brightness, lock, unlock",
        "parameters": {
            "type": "object",
            "properties": {
                "device_id": {
                    "type": "string",
                    "description": "The ID of the device to control"
                },
                "command": {
                    "type": "string",
                    "description": "The command: turn_on, turn_off, set_temperature, set_brightness, lock, unlock"
                },
                "parameters": {
                    "type": "object",
                    "description": "Additional parameters (e.g., {temperature: 72})"
                }
            },
            "required": ["device_id", "command"]
        }
    },
    {
        "name": "get_all_device_statuses",
        "description": "Get status of ALL devices at once. Use this instead of multiple get_device_status calls.",
        "parameters": {
            "type": "object",
            "properties": {}
        }
    },
    {
        "name": "create_automation",
        "description": "Create a new automation rule",
        "parameters": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "Name of the automation"},
                "trigger": {"type": "object", "description": "Trigger configuration"},
                "actions": {"type": "array", "description": "Actions to perform"}
            },
            "required": ["name", "trigger", "actions"]
        }
    },
    {
        "name": "get_analytics",
        "description": "Get analytics and insights about device usage",
        "parameters": {
            "type": "object",
            "properties": {
                "period": {"type": "string", "description": "Time period: day, week, month"}
            }
        }
    }
]


class AgenticAI:
    """Main AI agent class - optimized for minimal Gemini API calls."""

    def __init__(self):
        self.http_client = httpx.AsyncClient(timeout=30.0)

    async def close(self):
        await self.http_client.aclose()

    async def chat(self, user_id: str, message: str, conversation_id: Optional[str] = None) -> ChatResponse:
        """Process a chat message with optimized Gemini usage."""

        logger.info(f"Chat called with user_id={user_id}, message='{message}'")

        with chat_latency.time():
            try:
                if not conversation_id:
                    conversation_id = str(uuid4())

                if conversation_id not in conversations:
                    conversations[conversation_id] = []

                history = conversations[conversation_id]
                history.append({"role": "user", "parts": [{"text": message}]})

                # Try to handle locally first (no Gemini call)
                logger.info(f"About to try local handling...")
                local_result = await self._try_local_handling(user_id, message)
                logger.info(f"Local handling result: {local_result is not None}")
                if local_result:
                    response_text, actions_taken = local_result
                else:
                    # Need Gemini - make ONE call to get all tool calls
                    response_text, actions_taken = await self._call_gemini_optimized(user_id, message, history)

                history.append({"role": "model", "parts": [{"text": response_text}]})

                if len(history) > 20:
                    history = history[-20:]
                conversations[conversation_id] = history

                chat_requests.labels(status="success").inc()

                return ChatResponse(
                    id=str(uuid4()),
                    conversation_id=conversation_id,
                    message=response_text,
                    actions_taken=actions_taken,
                    suggestions=self._generate_suggestions(message, response_text),
                    timestamp=datetime.utcnow().isoformat()
                )

            except HTTPException as e:
                chat_requests.labels(status="error").inc()
                # Return error in response instead of raising
                return ChatResponse(
                    id=str(uuid4()),
                    conversation_id=conversation_id or str(uuid4()),
                    message="",
                    actions_taken=[],
                    suggestions=[],
                    timestamp=datetime.utcnow().isoformat(),
                    error=e.detail if hasattr(e, 'detail') else str(e)
                )
            except Exception as e:
                logger.error(f"Chat error: {e}")
                chat_requests.labels(status="error").inc()
                return ChatResponse(
                    id=str(uuid4()),
                    conversation_id=conversation_id or str(uuid4()),
                    message="",
                    actions_taken=[],
                    suggestions=[],
                    timestamp=datetime.utcnow().isoformat(),
                    error=str(e)
                )

    async def _try_local_handling(self, user_id: str, message: str) -> Optional[tuple[str, List[Dict]]]:
        """Try to handle common requests locally without calling Gemini."""

        msg_lower = message.lower().strip()
        actions_taken = []

        logger.info(f"Checking local handling for: '{msg_lower}'")

        # Pattern: List devices
        device_patterns = ["what devices", "list devices", "show devices", "my devices", "all devices", "do i have"]
        if any(p in msg_lower for p in device_patterns) and "status" not in msg_lower:
            result = await self._tool_list_devices(user_id, {})
            actions_taken.append({"tool": "list_devices", "args": {}, "result": result})
            response = self._format_device_list(result)
            return response, actions_taken

        # Pattern: All device status
        if any(p in msg_lower for p in ["status of all", "all device status", "all status", "status of my devices", "show me all device"]):
            result = await self._tool_get_all_device_statuses(user_id, {})
            actions_taken.append({"tool": "get_all_device_statuses", "args": {}, "result": result})
            response = self._format_all_device_statuses(result)
            return response, actions_taken

        # Pattern: Status of specific device (e.g., "status of front door", "is the door locked")
        status_patterns = ["status of", "what is the status", "is the", "check the", "how is the"]
        if any(p in msg_lower for p in status_patterns):
            # Extract device reference
            devices_result = await self._tool_list_devices(user_id, {})
            devices = devices_result.get("devices", [])
            for device in devices:
                device_name_lower = device.get("name", "").lower()
                device_type = device.get("type", "")
                # Check if device name or type words are in the message
                name_words = device_name_lower.split()
                if any(word in msg_lower for word in name_words if len(word) > 2):
                    # Found a matching device - get its status
                    status_result = await self._tool_get_device_status(user_id, {"device_id": device["id"]})
                    actions_taken.append({"tool": "list_devices", "args": {}, "result": devices_result})
                    actions_taken.append({"tool": "get_device_status", "args": {"device_id": device["id"]}, "result": status_result})
                    response = self._format_single_device_status(device, status_result)
                    return response, actions_taken

        # Pattern: Turn on/off ALL lights
        for action, cmd in [("turn on", "turn_on"), ("turn off", "turn_off"), ("switch on", "turn_on"), ("switch off", "turn_off")]:
            if action in msg_lower and "all" in msg_lower and "light" in msg_lower:
                devices_result = await self._tool_list_devices(user_id, {"device_type": "light"})
                lights = devices_result.get("devices", [])
                if lights:
                    for light in lights:
                        cmd_result = await self._tool_send_device_command(user_id, {
                            "device_id": light["id"],
                            "command": cmd
                        })
                        actions_taken.append({"tool": "send_device_command", "args": {"device_id": light["id"], "command": cmd}, "result": cmd_result})

                    state = "on" if cmd == "turn_on" else "off"
                    response = f"Done! I've turned {state} all {len(lights)} light(s)."
                    return response, actions_taken

        # Pattern: Turn on/off specific device
        for action, cmd in [("turn on", "turn_on"), ("turn off", "turn_off"), ("switch on", "turn_on"), ("switch off", "turn_off")]:
            if action in msg_lower:
                # Find device name in message
                device_name = self._extract_device_name(msg_lower, action)
                if device_name:
                    # Get device list to find ID
                    devices_result = await self._tool_list_devices(user_id, {})
                    device = self._find_device_by_name(devices_result.get("devices", []), device_name)
                    if device:
                        cmd_result = await self._tool_send_device_command(user_id, {
                            "device_id": device["id"],
                            "command": cmd
                        })
                        actions_taken.append({"tool": "list_devices", "args": {}, "result": devices_result})
                        actions_taken.append({"tool": "send_device_command", "args": {"device_id": device["id"], "command": cmd}, "result": cmd_result})

                        state = "on" if cmd == "turn_on" else "off"
                        response = f"Done! I've turned {state} the {device['name']}."
                        return response, actions_taken

        # Pattern: Lock/unlock door (check unlock first since "lock" is in "unlock")
        for action, cmd in [("unlock", "unlock"), ("lock", "lock")]:
            if action in msg_lower and ("door" in msg_lower or "lock" in msg_lower):
                devices_result = await self._tool_list_devices(user_id, {"device_type": "smart_lock"})
                devices = devices_result.get("devices", [])
                if devices:
                    device = devices[0]  # Use first lock found
                    cmd_result = await self._tool_send_device_command(user_id, {
                        "device_id": device["id"],
                        "command": cmd
                    })
                    actions_taken.append({"tool": "list_devices", "args": {"device_type": "smart_lock"}, "result": devices_result})
                    actions_taken.append({"tool": "send_device_command", "args": {"device_id": device["id"], "command": cmd}, "result": cmd_result})

                    response = f"Done! I've {action}ed the {device['name']}."
                    return response, actions_taken

        # Pattern: Set temperature
        if "temperature" in msg_lower or "thermostat" in msg_lower:
            import re
            temp_match = re.search(r'(\d+)\s*(?:degrees?|°|f)?', msg_lower)
            if temp_match and ("set" in msg_lower or "to" in msg_lower):
                temp = int(temp_match.group(1))
                devices_result = await self._tool_list_devices(user_id, {"device_type": "thermostat"})
                devices = devices_result.get("devices", [])
                if devices:
                    device = devices[0]
                    cmd_result = await self._tool_send_device_command(user_id, {
                        "device_id": device["id"],
                        "command": "set_temperature",
                        "parameters": {"temperature": temp}
                    })
                    actions_taken.append({"tool": "list_devices", "args": {"device_type": "thermostat"}, "result": devices_result})
                    actions_taken.append({"tool": "send_device_command", "args": {"device_id": device["id"], "command": "set_temperature", "parameters": {"temperature": temp}}, "result": cmd_result})

                    response = f"Done! I've set the {device['name']} to {temp}°F."
                    return response, actions_taken

        # Pattern: Get analytics
        if any(p in msg_lower for p in ["analytics", "usage", "activity", "summary", "insights"]):
            period = "day"
            if "week" in msg_lower:
                period = "week"
            elif "month" in msg_lower:
                period = "month"

            result = await self._tool_get_analytics(user_id, {"period": period})
            actions_taken.append({"tool": "get_analytics", "args": {"period": period}, "result": result})
            response = self._format_analytics(result)
            return response, actions_taken

        return None  # Need Gemini for complex requests

    def _extract_device_name(self, message: str, action: str) -> Optional[str]:
        """Extract device name from message."""
        # Remove the action words
        text = message.replace(action, "").strip()
        # Remove common words
        for word in ["the", "my", "please", "can you", "could you", "device", "for me"]:
            text = text.replace(word, "").strip()
        return text if text else None

    def _find_device_by_name(self, devices: List[Dict], name: str) -> Optional[Dict]:
        """Find device by partial name match."""
        name_lower = name.lower()
        for device in devices:
            device_name = device.get("name", "").lower()
            if name_lower in device_name or device_name in name_lower:
                return device
        return None

    def _format_device_list(self, result: Dict) -> str:
        """Format device list for display."""
        devices = result.get("devices", [])
        if not devices:
            return "You don't have any devices registered yet."

        lines = [f"You have {len(devices)} device(s):\n"]
        for d in devices:
            status = "🟢 online" if d.get("online") else "🔴 offline"
            lines.append(f"• **{d['name']}** ({d['type']}) - {status}")

        return "\n".join(lines)

    def _format_all_device_statuses(self, result: Dict) -> str:
        """Format all device statuses for display."""
        statuses = result.get("statuses", [])
        if not statuses:
            return "No devices found."

        # Group by location
        by_location = {}
        for s in statuses:
            loc = s.get("location", "Other")
            if loc not in by_location:
                by_location[loc] = []
            by_location[loc].append(s)

        lines = ["Here's the status of all your devices:\n"]
        for location, devices in by_location.items():
            lines.append(f"**{location}**")
            for d in devices:
                state = d.get("state", d.get("status", "unknown"))
                name = d.get("name", "Unknown")
                dtype = d.get("type", "")

                # Format based on device type
                if dtype == "light":
                    if d.get("state") == "on":
                        extra = f", brightness {d.get('brightness', 100)}%" if d.get('brightness') else ""
                        lines.append(f"  • {name}: 💡 On{extra}")
                    else:
                        lines.append(f"  • {name}: Off")
                elif dtype == "thermostat":
                    temp = d.get("target_temperature", d.get("config", {}).get("target_temp", "?"))
                    lines.append(f"  • {name}: 🌡️ {temp}°F")
                elif dtype == "smart_lock":
                    state_icon = "🔒 Locked" if d.get("state") == "locked" else "🔓 Unlocked"
                    lines.append(f"  • {name}: {state_icon}")
                elif dtype == "camera":
                    motion = "motion detection on" if d.get("motion_detection") else ""
                    lines.append(f"  • {name}: 📷 Active" + (f", {motion}" if motion else ""))
                elif dtype == "door_sensor":
                    online = "🟢" if d.get("online") else "🔴 offline"
                    lines.append(f"  • {name}: {online}")
                else:
                    lines.append(f"  • {name}: {state}")
            lines.append("")

        return "\n".join(lines)

    def _format_single_device_status(self, device: Dict, status_result: Dict) -> str:
        """Format single device status for display."""
        name = device.get("name", "Unknown")
        dtype = device.get("type", "")
        config = device.get("config", {})

        # Merge status_result into config if it has additional data
        if status_result and "device" in status_result:
            config.update(status_result.get("device", {}).get("config", {}))

        lines = [f"**{name}** status:\n"]

        if dtype == "smart_lock":
            locked = config.get("locked", False)
            state = "🔒 Locked" if locked else "🔓 Unlocked"
            lines.append(f"State: {state}")
        elif dtype == "light":
            on = config.get("power_on", False)
            state = "💡 On" if on else "Off"
            brightness = config.get("brightness")
            lines.append(f"State: {state}")
            if brightness:
                lines.append(f"Brightness: {brightness}%")
        elif dtype == "thermostat":
            temp = config.get("target_temp", "?")
            lines.append(f"🌡️ Target temperature: {temp}°F")
        elif dtype == "camera":
            lines.append("📷 Camera is active")
        elif dtype == "door_sensor":
            lines.append("🚪 Door sensor monitoring")
        else:
            lines.append(f"Status: {device.get('status', 'unknown')}")

        location = device.get("location")
        if location:
            lines.append(f"Location: {location}")

        return "\n".join(lines)

    def _format_analytics(self, result: Dict) -> str:
        """Format analytics for display."""
        summary = result.get("summary", {})
        top_devices = result.get("top_devices", [])

        lines = [
            f"**Activity Summary** ({result.get('period', 'day')})\n",
            f"• Total events: {summary.get('total_events', 0)}",
            f"• Active devices: {summary.get('active_devices', 0)}",
            f"• Alerts: {summary.get('alerts', 0)}",
            f"• Energy usage: {summary.get('energy_usage', 'N/A')}",
        ]

        if top_devices:
            lines.append("\n**Most Active Devices:**")
            for d in top_devices[:3]:
                lines.append(f"  • {d['name']}: {d['events']} events")

        return "\n".join(lines)

    async def _call_gemini_optimized(self, user_id: str, message: str, history: List[Dict]) -> tuple[str, List[Dict]]:
        """Call Gemini once to get all needed tool calls, execute them, generate response locally."""

        gemini_calls.labels(type="planning").inc()

        system_prompt = f"""You are HomeGuard AI. Analyze the user request and return the tool calls needed.

User ID: {user_id}

IMPORTANT: Return ALL tool calls needed in a single response. Don't wait for results.
For device control: Use list_devices first to get IDs, then send_device_command.
For status of all devices: Use get_all_device_statuses (one call, not multiple get_device_status).

Be concise. Execute actions immediately without asking for confirmation."""

        contents = [
            {"role": "user", "parts": [{"text": system_prompt}]},
            {"role": "model", "parts": [{"text": "Ready to help."}]}
        ] + history[-6:]  # Keep recent history short

        function_declarations = [
            {"name": t["name"], "description": t["description"], "parameters": t["parameters"]}
            for t in TOOLS
        ]

        request_body = {
            "contents": contents,
            "tools": [{"function_declarations": function_declarations}],
            "generationConfig": {
                "temperature": 0.3,  # Lower for more deterministic tool selection
                "maxOutputTokens": 512,
            }
        }

        url = f"{GEMINI_BASE_URL}/models/{GEMINI_TEXT_MODEL}:generateContent?key={GEMINI_TEXT_API_KEY}"

        # Call Gemini with retries
        response = await self._call_gemini_with_retry(url, request_body)
        if response is None:
            raise HTTPException(status_code=429, detail="AI service is busy. Please try again in a moment.")

        result = response.json()
        candidates = result.get("candidates", [])
        if not candidates:
            return "I couldn't understand that request. Please try again.", []

        content = candidates[0].get("content", {})
        parts = content.get("parts", [])

        # Collect all tool calls from the response
        tool_calls_to_execute = []
        response_text = ""

        for part in parts:
            if "text" in part:
                response_text = part["text"]
            elif "functionCall" in part:
                func_call = part["functionCall"]
                tool_calls_to_execute.append({
                    "name": func_call["name"],
                    "args": func_call.get("args", {})
                })

        # Execute all tools locally (no more Gemini calls)
        actions_taken = []
        tool_results = {}

        for tc in tool_calls_to_execute:
            logger.info(f"[Gemini path] Executing tool: {tc['name']} with args: {tc['args']}")
            tool_calls.labels(tool_name=tc['name']).inc()

            result = await self._execute_tool(user_id, tc['name'], tc['args'])
            actions_taken.append({
                "tool": tc['name'],
                "args": tc['args'],
                "result": result
            })
            tool_results[tc['name']] = result

        # Generate response locally based on what was executed
        if not response_text:
            response_text = self._generate_response_from_actions(actions_taken)

        return response_text, actions_taken

    async def _call_gemini_with_retry(self, url: str, request_body: Dict) -> Optional[httpx.Response]:
        """Call Gemini API with retry logic."""
        max_retries = 5
        retry_delay = 10.0

        for attempt in range(max_retries):
            try:
                response = await self.http_client.post(url, json=request_body)

                if response.status_code == 200:
                    return response
                elif response.status_code == 429:
                    if attempt < max_retries - 1:
                        logger.warning(f"Rate limited, retrying in {retry_delay}s (attempt {attempt + 1}/{max_retries})")
                        await asyncio.sleep(retry_delay)
                        retry_delay = min(retry_delay * 2, 60.0)
                    else:
                        logger.error("Gemini API rate limit exceeded")
                        return None
                else:
                    logger.error(f"Gemini API error: {response.status_code}")
                    return None
            except Exception as e:
                logger.error(f"Gemini API call failed: {e}")
                if attempt < max_retries - 1:
                    await asyncio.sleep(retry_delay)
                else:
                    return None

        return None

    def _generate_response_from_actions(self, actions: List[Dict]) -> str:
        """Generate a human-readable response from executed actions."""
        if not actions:
            return "I'm not sure how to help with that. Try asking about your devices or their status."

        responses = []

        for action in actions:
            tool = action["tool"]
            result = action.get("result", {})
            args = action.get("args", {})

            if tool == "list_devices":
                responses.append(self._format_device_list(result))

            elif tool == "get_all_device_statuses":
                responses.append(self._format_all_device_statuses(result))

            elif tool == "get_device_status":
                name = result.get("name", "Device")
                state = result.get("state", result.get("status", "unknown"))
                responses.append(f"**{name}**: {state}")

            elif tool == "send_device_command":
                if result.get("success"):
                    cmd = args.get("command", "command")
                    responses.append(f"✓ Command '{cmd}' executed successfully.")
                else:
                    responses.append(f"⚠️ Command failed: {result.get('error', 'Unknown error')}")

            elif tool == "get_analytics":
                responses.append(self._format_analytics(result))

            elif tool == "create_automation":
                if result.get("success"):
                    responses.append(f"✓ Automation created: {args.get('name', 'New automation')}")
                else:
                    responses.append(f"⚠️ Failed to create automation")

        return "\n\n".join(responses) if responses else "Done!"

    async def _execute_tool(self, user_id: str, tool_name: str, args: Dict) -> Dict:
        """Execute a tool and return the result."""
        try:
            if tool_name == "list_devices":
                return await self._tool_list_devices(user_id, args)
            elif tool_name == "get_device_status":
                return await self._tool_get_device_status(user_id, args)
            elif tool_name == "get_all_device_statuses":
                return await self._tool_get_all_device_statuses(user_id, args)
            elif tool_name == "send_device_command":
                return await self._tool_send_device_command(user_id, args)
            elif tool_name == "create_automation":
                return await self._tool_create_automation(user_id, args)
            elif tool_name == "get_analytics":
                return await self._tool_get_analytics(user_id, args)
            else:
                return {"error": f"Unknown tool: {tool_name}"}
        except Exception as e:
            logger.error(f"Tool execution error: {e}")
            return {"error": str(e)}

    async def _tool_list_devices(self, user_id: str, args: Dict) -> Dict:
        """List user's devices."""
        url = f"{DEVICE_SERVICE_URL}/devices"
        params = {}
        if args.get("device_type"):
            params["type"] = args["device_type"]

        response = await self.http_client.get(url, params=params, headers={"X-User-ID": user_id})
        if response.status_code == 200:
            return response.json()
        return {"devices": [], "error": "Failed to fetch devices"}

    async def _tool_get_device_status(self, user_id: str, args: Dict) -> Dict:
        """Get single device status."""
        device_id = args.get("device_id")
        url = f"{DEVICE_SERVICE_URL}/devices/{device_id}/status"
        response = await self.http_client.get(url, headers={"X-User-ID": user_id})
        if response.status_code == 200:
            return response.json()
        return {"error": "Device not found"}

    async def _tool_get_all_device_statuses(self, user_id: str, args: Dict) -> Dict:
        """Get status of ALL devices in one call - much more efficient."""
        # First get all devices
        devices_response = await self._tool_list_devices(user_id, {})
        devices = devices_response.get("devices", [])

        if not devices:
            return {"statuses": []}

        # Get status for each device concurrently
        async def get_status(device):
            try:
                url = f"{DEVICE_SERVICE_URL}/devices/{device['id']}/status"
                response = await self.http_client.get(url, headers={"X-User-ID": user_id})
                if response.status_code == 200:
                    return response.json()
                return {"name": device.get("name"), "status": "unknown", "error": "Failed to get status"}
            except Exception as e:
                return {"name": device.get("name"), "status": "error", "error": str(e)}

        statuses = await asyncio.gather(*[get_status(d) for d in devices])
        return {"statuses": list(statuses), "count": len(statuses)}

    async def _tool_send_device_command(self, user_id: str, args: Dict) -> Dict:
        """Send command to device."""
        device_id = args.get("device_id")
        url = f"{DEVICE_SERVICE_URL}/devices/{device_id}/command"

        response = await self.http_client.post(
            url,
            json={"command": args.get("command"), "payload": args.get("parameters", {})},
            headers={"X-User-ID": user_id}
        )

        if response.status_code in [200, 202]:
            return {"success": True, "message": f"Command '{args.get('command')}' executed"}
        return {"success": False, "error": "Failed to send command"}

    async def _tool_create_automation(self, user_id: str, args: Dict) -> Dict:
        """Create automation via n8n webhook."""
        # For now, return mock success
        return {"success": True, "message": f"Automation '{args.get('name')}' created", "id": str(uuid4())}

    async def _tool_get_analytics(self, user_id: str, args: Dict) -> Dict:
        """Get analytics summary."""
        return {
            "period": args.get("period", "day"),
            "summary": {
                "total_events": 142,
                "active_devices": 8,
                "alerts": 3,
                "energy_usage": "12.5 kWh"
            },
            "top_devices": [
                {"name": "Living Room Camera", "events": 45},
                {"name": "Front Door Sensor", "events": 32},
                {"name": "Thermostat", "events": 28}
            ]
        }

    def _generate_suggestions(self, user_message: str, response: str) -> List[str]:
        """Generate follow-up suggestions."""
        lower_msg = user_message.lower()

        if "device" in lower_msg or "status" in lower_msg:
            return ["Show offline devices", "Turn on all lights", "Lock all doors"]
        if "temperature" in lower_msg:
            return ["Set thermostat schedule", "Show energy usage", "Turn on AC"]
        if "security" in lower_msg or "camera" in lower_msg:
            return ["Show motion alerts", "Lock front door", "Arm security system"]

        return ["What's the status of my devices?", "Turn on living room lights", "Show today's activity"]


# Initialize the AI agent
agent = AgenticAI()


@app.on_event("shutdown")
async def shutdown():
    await agent.close()


@app.get("/health")
async def health_check():
    return {"status": "healthy"}


@app.get("/metrics")
async def metrics():
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)


@app.post("/agent/chat", response_model=ChatResponse)
async def chat(
    request: ChatRequest,
    x_user_id: str = Header(..., alias="X-User-ID")
):
    """Process a chat message from the user."""
    return await agent.chat(
        user_id=x_user_id,
        message=request.message,
        conversation_id=request.conversation_id
    )


@app.get("/agent/history")
async def get_history(
    conversation_id: str,
    x_user_id: str = Header(..., alias="X-User-ID")
):
    """Get conversation history."""
    if conversation_id not in conversations:
        return {"messages": []}
    return {"conversation_id": conversation_id, "messages": conversations[conversation_id]}


@app.delete("/agent/history")
async def clear_history(x_user_id: str = Header(..., alias="X-User-ID")):
    """Clear all conversation history for the user."""
    # Clear all conversations (in production, filter by user_id)
    conversations.clear()
    return {"status": "ok", "message": "History cleared"}


@app.get("/agent/suggestions")
async def get_suggestions(x_user_id: str = Header(..., alias="X-User-ID")):
    """Get suggested prompts."""
    return {
        "suggestions": [
            "What's the status of all my devices?",
            "Turn on the living room lights",
            "Set thermostat to 72 degrees",
            "Lock the front door",
            "Show today's activity"
        ]
    }


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8080"))
    uvicorn.run(app, host="0.0.0.0", port=port)
