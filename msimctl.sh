#!/bin/bash
#
# msimctl.sh - mSIM Server Management Script
#
# Usage:
#   ./msimctl.sh start       - Start the server
#   ./msimctl.sh stop        - Stop the server (interactive)
#   ./msimctl.sh stats       - Show server statistics
#   ./msimctl.sh status      - Check if server is running
#

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_BIN="${SCRIPT_DIR}/msim-server"
CONTROL_SOCKET="/tmp/msim.sock"
PID_FILE="/tmp/msim.pid"
LOG_FILE="${SCRIPT_DIR}/msim.log"

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
    echo -e "${BLUE}║${NC}${BOLD}          mSIM Server Management            ${NC}${BLUE}║${NC}"
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

# Check if server is running
is_running() {
    if [[ -S "${CONTROL_SOCKET}" ]]; then
        return 0
    fi
    if [[ -f "${PID_FILE}" ]]; then
        local pid
        pid=$(cat "${PID_FILE}")
        if kill -0 "${pid}" 2>/dev/null; then
            return 0
        fi
    fi
    return 1
}

# Get server PID
get_pid() {
    if [[ -f "${PID_FILE}" ]]; then
        cat "${PID_FILE}"
    else
        echo ""
    fi
}

# Send command to control socket
send_command() {
    local cmd="$1"
    if [[ ! -S "${CONTROL_SOCKET}" ]]; then
        print_error "Control socket not found. Is the server running?"
        return 1
    fi
    echo "${cmd}" | nc -U "${CONTROL_SOCKET}" 2>/dev/null
}

# Start server
cmd_start() {
    print_header
    echo -e "${BOLD}Starting mSIM Server${NC}"
    echo

    if is_running; then
        print_warning "Server is already running (PID: $(get_pid))"
        return 1
    fi

    # Build if binary doesn't exist
    if [[ ! -f "${SERVER_BIN}" ]]; then
        print_info "Building server..."
        (cd "${SCRIPT_DIR}" && go build -o msim-server .)
        if [[ $? -ne 0 ]]; then
            print_error "Failed to build server"
            return 1
        fi
        print_success "Server built successfully"
    fi

    # Start server
    print_info "Starting server..."
    nohup "${SERVER_BIN}" >> "${LOG_FILE}" 2>&1 &
    local pid=$!
    echo "${pid}" > "${PID_FILE}"

    # Wait for socket to appear
    local attempts=0
    while [[ ! -S "${CONTROL_SOCKET}" ]] && [[ ${attempts} -lt 30 ]]; do
        sleep 0.1
        ((attempts++))
    done

    if is_running; then
        print_success "Server started (PID: ${pid})"
        print_info "Log file: ${LOG_FILE}"
    else
        print_error "Failed to start server"
        return 1
    fi
}

# Stop server
cmd_stop() {
    print_header
    echo -e "${BOLD}Stopping mSIM Server${NC}"
    echo

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
            completion_time=$(date -u -d "+${downtime_input} minutes" "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
                              date -u -v+${downtime_input}M "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null)
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
    print_info "Sending shutdown command to server..."

    local cmd="shutdown|${reason}|${completion_time}"
    local response
    response=$(send_command "${cmd}" 2>&1) || true

    # Wait for server to stop
    sleep 0.5

    if ! is_running; then
        print_success "Server stopped successfully"
        rm -f "${PID_FILE}"
    else
        print_warning "Server may still be running, sending SIGTERM..."
        local pid
        pid=$(get_pid)
        if [[ -n "${pid}" ]]; then
            kill -TERM "${pid}" 2>/dev/null || true
            sleep 1
            if ! is_running; then
                print_success "Server stopped"
                rm -f "${PID_FILE}"
            else
                print_error "Failed to stop server, try: kill -9 ${pid}"
                return 1
            fi
        fi
    fi
}

# Show statistics
cmd_stats() {
    print_header
    echo -e "${BOLD}Server Statistics${NC}"
    echo

    if ! is_running; then
        print_error "Server is not running"
        return 1
    fi

    local response
    response=$(send_command "stats")

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
        print_error "Failed to get statistics: ${response}"
        return 1
    fi
}

# Show status
cmd_status() {
    print_header
    echo -e "${BOLD}Server Status${NC}"
    echo

    if is_running; then
        local pid
        pid=$(get_pid)
        print_success "Server is running (PID: ${pid:-unknown})"
        echo
        echo -e "  Control socket: ${CYAN}${CONTROL_SOCKET}${NC}"
        echo -e "  Log file: ${CYAN}${LOG_FILE}${NC}"
    else
        print_warning "Server is not running"
    fi
}

# Show usage
usage() {
    echo "Usage: $0 {start|stop|stats|status}"
    echo
    echo "Commands:"
    echo "  start   - Start the mSIM server"
    echo "  stop    - Stop the server (interactive, with reason selection)"
    echo "  stats   - Show server statistics"
    echo "  status  - Check if server is running"
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
    *)
        usage
        exit 1
        ;;
esac

