#!/bin/bash
#
# msimctl.sh - mSIM Server Management Script
#
# Usage:
#   ./msimctl.sh start       - Start the server
#   ./msimctl.sh stop        - Stop the server (interactive)
#   ./msimctl.sh stats       - Show server statistics
#   ./msimctl.sh status      - Check if server is running
#   ./msimctl.sh logs        - Show server logs
#   ./msimctl.sh restart     - Restart the server
#

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER_NAME="msim-server"
CONTROL_SOCKET="/tmp/msim.sock"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Print functions
print_header() {
    echo -e "${BLUE}╔════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC}${BOLD}     mSIM Server Management (Docker)        ${NC}${BLUE}║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════╝${NC}"
    echo
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1" >&2
}

print_warning() {
    echo -e "${YELLOW}!${NC} $1"
}

print_info() {
    echo -e "${CYAN}ℹ${NC} $1"
}

# Check if Docker is available
check_docker() {
    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed or not in PATH"
        exit 1
    fi
    if ! docker info &> /dev/null; then
        print_error "Docker daemon is not running"
        exit 1
    fi
}

# Check if container is running
is_running() {
    docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_NAME}$"
}

# Check if container exists (running or stopped)
container_exists() {
    docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^${CONTAINER_NAME}$"
}

# Send command to control socket inside container
send_command() {
    local cmd="$1"
    docker exec "${CONTAINER_NAME}" sh -c "echo '${cmd}' | nc -U ${CONTROL_SOCKET}" 2>/dev/null
}

# Start server
cmd_start() {
    print_header
    echo -e "${BOLD}Starting mSIM Server (Docker)${NC}"
    echo

    check_docker

    if is_running; then
        print_warning "Server is already running"
        docker ps --filter "name=${CONTAINER_NAME}" --format "  Container: {{.Names}}\n  Status: {{.Status}}\n  Ports: {{.Ports}}"
        return 0
    fi

    print_info "Building and starting container..."
    
    if ! docker compose -f "${COMPOSE_FILE}" up -d --build 2>&1; then
        print_error "Failed to start container"
        return 1
    fi

    # Wait for container to be healthy
    print_info "Waiting for server to be ready..."
    local attempts=0
    while [[ ${attempts} -lt 30 ]]; do
        if is_running; then
            local health
            health=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER_NAME}" 2>/dev/null || echo "none")
            if [[ "${health}" == "healthy" ]] || [[ "${health}" == "none" ]]; then
                break
            fi
        fi
        sleep 1
        ((attempts++))
    done

    if is_running; then
        print_success "Server started successfully"
        echo
        docker ps --filter "name=${CONTAINER_NAME}" --format "  Container: {{.Names}}
  Status: {{.Status}}
  Ports: {{.Ports}}"
    else
        print_error "Failed to start server"
        print_info "Check logs with: ./msimctl.sh logs"
        return 1
    fi
}

# Stop server
cmd_stop() {
    print_header
    echo -e "${BOLD}Stopping mSIM Server${NC}"
    echo

    check_docker

    if ! is_running; then
        print_warning "Server is not running"
        return 1
    fi

    # Select reason
    echo -e "${BOLD}Select shutdown reason:${NC}"
    echo
    echo -e "  ${CYAN}1${NC}) maintenance - Server maintenance"
    echo -e "  ${CYAN}2${NC}) restart     - Server restart"
    echo -e "  ${CYAN}3${NC}) timeout     - Timeout (no completion time)"
    echo
    read -p "Enter choice [1-3]: " reason_choice

    local reason
    local need_time=true

    case "${reason_choice}" in
        1)
            reason="maintenance"
            ;;
        2)
            reason="restart"
            ;;
        3)
            reason="timeout"
            need_time=false
            ;;
        *)
            print_error "Invalid choice"
            return 1
            ;;
    esac

    local completion_time=""

    if [[ "${need_time}" == "true" ]]; then
        echo
        echo -e "${BOLD}Enter expected downtime:${NC}"
        echo
        echo -e "  Enter duration in minutes, or"
        echo -e "  Enter specific time in format: YYYY-MM-DDTHH:MM:SSZ"
        echo -e "  Examples: ${CYAN}5${NC} (5 minutes), ${CYAN}2024-12-21T22:00:00Z${NC}"
        echo
        read -p "Downtime: " downtime_input

        if [[ "${downtime_input}" =~ ^[0-9]+$ ]]; then
            # Minutes - calculate completion time
            # Try GNU date first, then BSD date (macOS)
            completion_time=$(date -u -d "+${downtime_input} minutes" "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
                              date -u -v+${downtime_input}M "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
                              echo "")
        else
            # Assume it's already in ISO format
            completion_time="${downtime_input}"
        fi
    fi

    echo
    echo -e "${BOLD}Shutdown summary:${NC}"
    echo -e "  Reason: ${CYAN}${reason}${NC}"
    if [[ -n "${completion_time}" ]]; then
        echo -e "  Completion time: ${CYAN}${completion_time}${NC}"
    fi
    echo

    read -p "Proceed with shutdown? [y/N]: " confirm
    if [[ "${confirm}" != "y" && "${confirm}" != "Y" ]]; then
        print_warning "Shutdown cancelled"
        return 1
    fi

    echo
    print_info "Sending shutdown command to connected clients..."

    # Try to send graceful shutdown command via control socket
    local cmd="shutdown|${reason}|${completion_time}"
    send_command "${cmd}" 2>/dev/null || true

    # Give time for bye messages to be sent
    sleep 1

    print_info "Stopping container..."
    docker compose -f "${COMPOSE_FILE}" stop

    if ! is_running; then
        print_success "Server stopped successfully"
    else
        print_warning "Container may still be running, forcing stop..."
        docker compose -f "${COMPOSE_FILE}" kill
        print_success "Server stopped"
    fi
}

# Show statistics
cmd_stats() {
    print_header
    echo -e "${BOLD}Server Statistics${NC}"
    echo

    check_docker

    if ! is_running; then
        print_error "Server is not running"
        return 1
    fi

    # Get container stats
    echo -e "${BOLD}Container Info:${NC}"
    docker ps --filter "name=${CONTAINER_NAME}" --format "  Name: {{.Names}}
  Status: {{.Status}}
  Ports: {{.Ports}}"
    echo

    # Try to get application stats via control socket
    local response
    response=$(send_command "stats" 2>/dev/null) || response=""

    if [[ "${response}" == OK* ]]; then
        local stats="${response#OK|}"
        stats="${stats%$'\n'}"

        # Parse stats
        local connections=""
        local users=""

        IFS=',' read -ra parts <<< "${stats}"
        for part in "${parts[@]}"; do
            if [[ "${part}" == connections=* ]]; then
                connections="${part#connections=}"
            elif [[ "${part}" == users=* ]]; then
                users="${part#users=}"
            fi
        done

        echo -e "┌────────────────────────────────────────────┐"
        echo -e "│ ${BOLD}Active Connections:${NC} ${GREEN}${connections}${NC}"
        echo -e "├────────────────────────────────────────────┤"

        if [[ -n "${users}" && "${users}" != "" ]]; then
            echo -e "│ ${BOLD}Online Users:${NC}"
            IFS=';' read -ra user_list <<< "${users}"
            for user in "${user_list[@]}"; do
                if [[ -n "${user}" ]]; then
                    echo -e "│   ${CYAN}●${NC} ${user}"
                fi
            done
        else
            echo -e "│ ${BOLD}Online Users:${NC} ${YELLOW}none${NC}"
        fi

        echo -e "└────────────────────────────────────────────┘"
    else
        print_warning "Could not get application stats (control socket not available)"
        echo
        echo -e "${BOLD}Container Resource Usage:${NC}"
        docker stats "${CONTAINER_NAME}" --no-stream --format "  CPU: {{.CPUPerc}}\n  Memory: {{.MemUsage}}\n  Network I/O: {{.NetIO}}"
    fi
}

# Show status
cmd_status() {
    print_header
    echo -e "${BOLD}Server Status${NC}"
    echo

    check_docker

    if is_running; then
        print_success "Server is running"
        echo
        docker ps --filter "name=${CONTAINER_NAME}" --format "  Container: {{.Names}}
  Image: {{.Image}}
  Status: {{.Status}}
  Ports: {{.Ports}}
  Created: {{.CreatedAt}}"
        
        # Check health status
        local health
        health=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER_NAME}" 2>/dev/null || echo "none")
        if [[ "${health}" != "none" ]]; then
            echo -e "  Health: ${health}"
        fi
    elif container_exists; then
        print_warning "Container exists but is not running"
        docker ps -a --filter "name=${CONTAINER_NAME}" --format "  Status: {{.Status}}"
    else
        print_warning "Server is not running (no container found)"
    fi
}

# Show logs
cmd_logs() {
    print_header
    echo -e "${BOLD}Server Logs${NC}"
    echo

    check_docker

    if ! container_exists; then
        print_error "Container does not exist"
        return 1
    fi

    local lines="${2:-50}"
    print_info "Showing last ${lines} log lines (Ctrl+C to exit follow mode)"
    echo
    docker compose -f "${COMPOSE_FILE}" logs --tail="${lines}" -f
}

# Restart server
cmd_restart() {
    print_header
    echo -e "${BOLD}Restarting mSIM Server${NC}"
    echo

    check_docker

    if is_running; then
        print_info "Sending restart notification to clients..."
        
        # Calculate completion time (1 minute from now)
        local completion_time
        completion_time=$(date -u -d "+1 minutes" "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
                          date -u -v+1M "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
                          echo "")
        
        send_command "shutdown|restart|${completion_time}" 2>/dev/null || true
        sleep 1
    fi

    print_info "Restarting container..."
    docker compose -f "${COMPOSE_FILE}" restart

    # Wait for container to be healthy
    local attempts=0
    while [[ ${attempts} -lt 30 ]]; do
        if is_running; then
            break
        fi
        sleep 1
        ((attempts++))
    done

    if is_running; then
        print_success "Server restarted successfully"
    else
        print_error "Failed to restart server"
        return 1
    fi
}

# Show usage
usage() {
    echo "Usage: $0 {start|stop|stats|status|logs|restart}"
    echo
    echo "Commands:"
    echo "  start   - Start the mSIM server (Docker)"
    echo "  stop    - Stop the server (interactive, with reason selection)"
    echo "  stats   - Show server statistics"
    echo "  status  - Check if server is running"
    echo "  logs    - Show server logs (follow mode)"
    echo "  restart - Restart the server"
    echo
}

# Main
case "${1:-}" in
    start)
        cmd_start
        ;;
    stop)
        cmd_stop
        ;;
    stats)
        cmd_stats
        ;;
    status)
        cmd_status
        ;;
    logs)
        cmd_logs "$@"
        ;;
    restart)
        cmd_restart
        ;;
    *)
        usage
        exit 1
        ;;
esac
