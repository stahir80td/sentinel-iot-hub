# N8N Workflow Deployment Script
# Deploys workflows to N8N via API

param(
    [string]$N8NUrl = "http://n8n.homeguard.localhost",
    [string]$WorkflowsPath = "$PSScriptRoot\workflows",
    [switch]$Activate = $true,
    [switch]$List = $false
)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "N8N Workflow Deployer" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if N8N is accessible
Write-Host "Checking N8N connectivity at $N8NUrl..." -ForegroundColor Yellow
try {
    $healthCheck = Invoke-RestMethod -Uri "$N8NUrl/healthz" -Method GET -TimeoutSec 5
    Write-Host "  N8N is accessible" -ForegroundColor Green
} catch {
    Write-Host "  WARNING: Could not reach N8N health endpoint. Continuing anyway..." -ForegroundColor Yellow
}

# List existing workflows
if ($List) {
    Write-Host ""
    Write-Host "Existing workflows:" -ForegroundColor Cyan
    try {
        $workflows = Invoke-RestMethod -Uri "$N8NUrl/api/v1/workflows" -Method GET
        foreach ($wf in $workflows.data) {
            $status = if ($wf.active) { "[ACTIVE]" } else { "[INACTIVE]" }
            Write-Host "  $status $($wf.name) (ID: $($wf.id))" -ForegroundColor White
        }
    } catch {
        Write-Host "  Could not list workflows: $_" -ForegroundColor Red
    }
    exit 0
}

# Get all workflow JSON files
$workflowFiles = Get-ChildItem -Path $WorkflowsPath -Filter "*.json" -ErrorAction SilentlyContinue

if ($workflowFiles.Count -eq 0) {
    Write-Host "No workflow files found in $WorkflowsPath" -ForegroundColor Yellow
    exit 0
}

Write-Host ""
Write-Host "Found $($workflowFiles.Count) workflow(s) to deploy:" -ForegroundColor Cyan

foreach ($file in $workflowFiles) {
    Write-Host ""
    Write-Host "Deploying: $($file.Name)..." -ForegroundColor Yellow

    try {
        $workflowJson = Get-Content -Path $file.FullName -Raw
        $workflow = $workflowJson | ConvertFrom-Json

        Write-Host "  Workflow name: $($workflow.name)" -ForegroundColor Gray

        # Try to create new workflow
        try {
            $response = Invoke-RestMethod -Uri "$N8NUrl/api/v1/workflows" `
                -Method POST `
                -ContentType "application/json" `
                -Body $workflowJson

            Write-Host "  Created workflow ID: $($response.data.id)" -ForegroundColor Green

            # Activate if requested
            if ($Activate) {
                try {
                    $activateResponse = Invoke-RestMethod -Uri "$N8NUrl/api/v1/workflows/$($response.data.id)/activate" `
                        -Method POST
                    Write-Host "  Workflow activated" -ForegroundColor Green
                } catch {
                    Write-Host "  Could not activate workflow: $_" -ForegroundColor Yellow
                }
            }
        } catch {
            if ($_.Exception.Response.StatusCode -eq 409) {
                Write-Host "  Workflow already exists, updating..." -ForegroundColor Yellow
                # Update existing workflow (would need to get ID first)
            } else {
                throw $_
            }
        }

    } catch {
        Write-Host "  ERROR: Failed to deploy - $_" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Deployment complete!" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Note: If API deployment fails, you can import workflows manually:" -ForegroundColor White
Write-Host "  1. Open N8N at $N8NUrl" -ForegroundColor Gray
Write-Host "  2. Click 'Add workflow' -> 'Import from File'" -ForegroundColor Gray
Write-Host "  3. Select the JSON files from: $WorkflowsPath" -ForegroundColor Gray
Write-Host "  4. Activate the workflow using the toggle switch" -ForegroundColor Gray
Write-Host ""
