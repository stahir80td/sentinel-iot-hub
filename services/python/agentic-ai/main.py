"""
HomeGuard Agentic AI Service

This service provides an AI-powered assistant for the HomeGuard IoT platform.
It uses Google Gemini for natural language understanding and tool execution.
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

DEVICE_SERVICE_URL = os.getenv("DEVICE_SERVICE_URL", "http://device-service:8080")
NOTIFICATION_SERVICE_URL = os.getenv("NOTIFICATION_SERVICE_URL", "http://notification-service:8080")
N8N_WEBHOOK_BASE = os.getenv("N8N_WEBHOOK_BASE", "http://n8n.homeguard-automation:5678/webhook")

# Metrics
chat_requests = Counter("agentic_ai_chat_requests_total", "Total chat requests", ["status"])
chat_latency = Histogram("agentic_ai_chat_latency_seconds", "Chat request latency")
tool_calls = Counter("agentic_ai_tool_calls_total", "Tool calls made", ["tool_name"])

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


class ActionRequest(BaseModel):
    action: str
    parameters: Dict[str, Any] = {}


# In-memory conversation storage (would use MongoDB in production)
conversations: Dict[str, List[Dict]] = {}


# Tool definitions for Gemini function calling
TOOLS = [
    {
        "name": "list_devices",
        "description": "Get a list of all IoT devices for the current user",
        "parameters": {
            "type": "object",
            "properties": {
                "device_type": {
                    "type": "string",
                    "description": "Optional filter by device type (e.g., 'sensor', 'camera', 'thermostat')"
                }
            }
        }
    },
    {
        "name": "get_device_status",
        "description": "Get the current status of a specific device",
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
        "description": "Send a command to control a device (e.g., turn on/off, set temperature)",
        "parameters": {
            "type": "object",
            "properties": {
                "device_id": {
                    "type": "string",
                    "description": "The ID of the device to control"
                },
                "command": {
                    "type": "string",
                    "description": "The command to send (e.g., 'turn_on', 'turn_off', 'set_temperature')"
                },
                "parameters": {
                    "type": "object",
                    "description": "Additional parameters for the command"
                }
            },
            "required": ["device_id", "command"]
        }
    },
    {
        "name": "create_automation",
        "description": "Create a new automation rule (e.g., 'turn on lights when motion detected')",
        "parameters": {
            "type": "object",
            "properties": {
                "name": {
                    "type": "string",
                    "description": "Name of the automation"
                },
                "trigger": {
                    "type": "object",
                    "description": "What triggers the automation",
                    "properties": {
                        "type": {"type": "string", "description": "Trigger type (e.g., 'device_event', 'schedule')"},
                        "device_id": {"type": "string", "description": "Device ID for device-based triggers"},
                        "event": {"type": "string", "description": "Event name to trigger on"}
                    }
                },
                "actions": {
                    "type": "array",
                    "description": "Actions to perform when triggered",
                    "items": {
                        "type": "object",
                        "properties": {
                            "type": {"type": "string", "description": "Action type (e.g., 'device_command', 'notification')"},
                            "device_id": {"type": "string", "description": "Target device ID"},
                            "command": {"type": "string", "description": "Command to send"}
                        }
                    }
                }
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
                "period": {
                    "type": "string",
                    "description": "Time period for analytics (e.g., 'day', 'week', 'month')"
                },
                "device_id": {
                    "type": "string",
                    "description": "Optional specific device ID"
                }
            }
        }
    }
]


class AgenticAI:
    """Main AI agent class that handles conversations and tool execution."""

    def __init__(self):
        self.http_client = httpx.AsyncClient(timeout=30.0)

    async def close(self):
        await self.http_client.aclose()

    async def chat(self, user_id: str, message: str, conversation_id: Optional[str] = None) -> ChatResponse:
        """Process a chat message and return a response."""

        with chat_latency.time():
            try:
                # Get or create conversation
                if not conversation_id:
                    conversation_id = str(uuid4())

                if conversation_id not in conversations:
                    conversations[conversation_id] = []

                # Build conversation history
                history = conversations[conversation_id]

                # Add user message to history
                history.append({
                    "role": "user",
                    "parts": [{"text": message}]
                })

                # Build the system prompt
                system_prompt = self._build_system_prompt(user_id)

                # Call Gemini API
                response_text, actions_taken = await self._call_gemini(
                    user_id, system_prompt, history
                )

                # Add assistant response to history
                history.append({
                    "role": "model",
                    "parts": [{"text": response_text}]
                })

                # Keep conversation history manageable
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

            except Exception as e:
                logger.error(f"Chat error: {e}")
                chat_requests.labels(status="error").inc()
                raise HTTPException(status_code=500, detail=str(e))

    def _build_system_prompt(self, user_id: str) -> str:
        """Build the system prompt for the AI."""
        return f"""You are HomeGuard AI, a helpful smart home assistant for the HomeGuard IoT platform.

Your capabilities:
1. List and manage IoT devices (sensors, cameras, thermostats, lights, etc.)
2. Check device status and control devices
3. Create automation rules
4. Provide analytics and insights about home usage
5. Help troubleshoot device issues
6. Suggest optimizations for energy efficiency and security

Current user ID: {user_id}

Guidelines:
- Be helpful, concise, and friendly
- When controlling devices, confirm the action before executing
- Proactively suggest relevant automations based on user patterns
- Prioritize security and safety in all recommendations
- If unsure about a command, ask for clarification

Always respond in a natural, conversational way while being informative."""

    async def _call_gemini(
        self,
        user_id: str,
        system_prompt: str,
        history: List[Dict]
    ) -> tuple[str, List[Dict]]:
        """Call the Gemini API with function calling support."""

        actions_taken = []

        # Build the request
        contents = [
            {"role": "user", "parts": [{"text": system_prompt}]},
            {"role": "model", "parts": [{"text": "I understand. I'm HomeGuard AI, ready to help you manage your smart home."}]}
        ] + history

        # Build function declarations
        function_declarations = [
            {
                "name": tool["name"],
                "description": tool["description"],
                "parameters": tool["parameters"]
            }
            for tool in TOOLS
        ]

        request_body = {
            "contents": contents,
            "tools": [{
                "function_declarations": function_declarations
            }],
            "generationConfig": {
                "temperature": 0.7,
                "topK": 40,
                "topP": 0.95,
                "maxOutputTokens": 1024,
            }
        }

        # Call Gemini API with retry logic for rate limits
        url = f"{GEMINI_BASE_URL}/models/{GEMINI_TEXT_MODEL}:generateContent?key={GEMINI_TEXT_API_KEY}"

        max_retries = 3
        retry_delay = 5.0  # Start with 5 seconds

        for attempt in range(max_retries):
            response = await self.http_client.post(url, json=request_body)

            if response.status_code == 200:
                break
            elif response.status_code == 429:
                # Rate limited - wait and retry
                if attempt < max_retries - 1:
                    logger.warning(f"Rate limited by Gemini API, retrying in {retry_delay}s (attempt {attempt + 1}/{max_retries})")
                    await asyncio.sleep(retry_delay)
                    retry_delay *= 2  # Exponential backoff
                else:
                    logger.error(f"Gemini API rate limit exceeded after {max_retries} attempts")
                    raise HTTPException(status_code=429, detail="AI service rate limited. Please wait a moment and try again.")
            else:
                logger.error(f"Gemini API error: {response.status_code} - {response.text}")
                raise HTTPException(status_code=502, detail="AI service unavailable")

        result = response.json()

        # Process the response
        candidates = result.get("candidates", [])
        if not candidates:
            return "I apologize, but I couldn't generate a response. Please try again.", []

        content = candidates[0].get("content", {})
        parts = content.get("parts", [])

        response_text = ""

        for part in parts:
            if "text" in part:
                response_text += part["text"]
            elif "functionCall" in part:
                # Handle function call
                func_call = part["functionCall"]
                func_name = func_call["name"]
                func_args = func_call.get("args", {})

                logger.info(f"Executing tool: {func_name} with args: {func_args}")
                tool_calls.labels(tool_name=func_name).inc()

                # Execute the tool
                tool_result = await self._execute_tool(user_id, func_name, func_args)
                actions_taken.append({
                    "tool": func_name,
                    "args": func_args,
                    "result": tool_result
                })

                # Make another call with the function result
                follow_up_response = await self._call_gemini_with_result(
                    user_id, system_prompt, history, func_name, tool_result
                )
                response_text = follow_up_response

        return response_text, actions_taken

    async def _call_gemini_with_result(
        self,
        user_id: str,
        system_prompt: str,
        history: List[Dict],
        func_name: str,
        result: Dict
    ) -> str:
        """Call Gemini with the function execution result."""

        contents = [
            {"role": "user", "parts": [{"text": system_prompt}]},
            {"role": "model", "parts": [{"text": "I understand. I'm HomeGuard AI."}]}
        ] + history + [
            {"role": "model", "parts": [{"functionCall": {"name": func_name}}]},
            {"role": "user", "parts": [{"functionResponse": {"name": func_name, "response": result}}]}
        ]

        request_body = {
            "contents": contents,
            "generationConfig": {
                "temperature": 0.7,
                "maxOutputTokens": 1024,
            }
        }

        url = f"{GEMINI_BASE_URL}/models/{GEMINI_TEXT_MODEL}:generateContent?key={GEMINI_TEXT_API_KEY}"

        # Retry logic for rate limits
        max_retries = 3
        retry_delay = 5.0

        for attempt in range(max_retries):
            response = await self.http_client.post(url, json=request_body)

            if response.status_code == 200:
                break
            elif response.status_code == 429:
                if attempt < max_retries - 1:
                    logger.warning(f"Rate limited on follow-up, retrying in {retry_delay}s")
                    await asyncio.sleep(retry_delay)
                    retry_delay *= 2
                else:
                    return f"I executed {func_name} successfully, but the AI service is busy. Please try again in a moment."
            else:
                return f"I executed {func_name} successfully, but had trouble generating a summary."

        result = response.json()
        candidates = result.get("candidates", [])
        if candidates:
            parts = candidates[0].get("content", {}).get("parts", [])
            for part in parts:
                if "text" in part:
                    return part["text"]

        return "Action completed successfully."

    async def _execute_tool(self, user_id: str, tool_name: str, args: Dict) -> Dict:
        """Execute a tool and return the result."""

        try:
            if tool_name == "list_devices":
                return await self._tool_list_devices(user_id, args)
            elif tool_name == "get_device_status":
                return await self._tool_get_device_status(user_id, args)
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

        response = await self.http_client.get(
            url,
            params=params,
            headers={"X-User-ID": user_id}
        )

        if response.status_code == 200:
            return response.json()
        return {"devices": [], "error": "Failed to fetch devices"}

    async def _tool_get_device_status(self, user_id: str, args: Dict) -> Dict:
        """Get device status."""
        device_id = args.get("device_id")
        url = f"{DEVICE_SERVICE_URL}/devices/{device_id}/status"

        response = await self.http_client.get(
            url,
            headers={"X-User-ID": user_id}
        )

        if response.status_code == 200:
            return response.json()
        return {"error": "Device not found or inaccessible"}

    async def _tool_send_device_command(self, user_id: str, args: Dict) -> Dict:
        """Send command to device."""
        device_id = args.get("device_id")
        url = f"{DEVICE_SERVICE_URL}/devices/{device_id}/command"

        response = await self.http_client.post(
            url,
            json={
                "command": args.get("command"),
                "payload": args.get("parameters", {})
            },
            headers={"X-User-ID": user_id}
        )

        if response.status_code in [200, 202]:
            return {"success": True, "message": f"Command '{args.get('command')}' sent to device"}
        return {"success": False, "error": "Failed to send command"}

    async def _tool_create_automation(self, user_id: str, args: Dict) -> Dict:
        """Create automation via n8n webhook."""
        url = f"{N8N_WEBHOOK_BASE}/create-automation"

        response = await self.http_client.post(
            url,
            json={
                "user_id": user_id,
                "name": args.get("name"),
                "trigger": args.get("trigger"),
                "actions": args.get("actions")
            }
        )

        if response.status_code in [200, 201]:
            return {"success": True, "message": f"Automation '{args.get('name')}' created"}
        return {"success": False, "error": "Failed to create automation"}

    async def _tool_get_analytics(self, user_id: str, args: Dict) -> Dict:
        """Get analytics summary."""
        # Return mock analytics for now
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
        suggestions = []

        lower_msg = user_message.lower()

        if "device" in lower_msg or "status" in lower_msg:
            suggestions.append("Show me all offline devices")
            suggestions.append("Create an alert for device disconnections")

        if "temperature" in lower_msg or "thermostat" in lower_msg:
            suggestions.append("Set up a schedule for the thermostat")
            suggestions.append("Show energy usage for heating/cooling")

        if "security" in lower_msg or "camera" in lower_msg:
            suggestions.append("Show recent motion alerts")
            suggestions.append("Arm the security system")

        if not suggestions:
            suggestions = [
                "What devices do I have?",
                "Show me today's activity",
                "Create a new automation"
            ]

        return suggestions[:3]


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


@app.post("/agent/stream")
async def stream_chat(
    request: ChatRequest,
    x_user_id: str = Header(..., alias="X-User-ID")
):
    """Stream a chat response (Server-Sent Events)."""
    # For now, return non-streaming response
    # Full streaming would require Gemini streaming API
    response = await agent.chat(
        user_id=x_user_id,
        message=request.message,
        conversation_id=request.conversation_id
    )
    return response


@app.get("/agent/history")
async def get_history(
    conversation_id: str,
    x_user_id: str = Header(..., alias="X-User-ID")
):
    """Get conversation history."""
    if conversation_id not in conversations:
        return {"messages": []}

    return {
        "conversation_id": conversation_id,
        "messages": conversations[conversation_id]
    }


@app.get("/agent/suggestions")
async def get_suggestions(x_user_id: str = Header(..., alias="X-User-ID")):
    """Get suggested prompts for the user."""
    return {
        "suggestions": [
            "What's the status of my devices?",
            "Show me today's activity summary",
            "Turn on the living room lights",
            "Create an automation for night mode",
            "Are there any security alerts?"
        ]
    }


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8080"))
    uvicorn.run(app, host="0.0.0.0", port=port)
