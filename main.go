package main

import (
	"bufio"
	"log"
	"msim/config"
	"msim/db"
	"msim/server"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const controlSocketPath = "/tmp/msim.sock"

func main() {
	cfg := config.Load()

	database, err := db.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	srvConfig := &server.ServerConfig{
		Port:               cfg.Port,
		ReadTimeout:        time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout:       time.Duration(cfg.WriteTimeout) * time.Second,
		FilePortRangeStart: cfg.FilePortRangeStart,
		FilePortRangeEnd:   cfg.FilePortRangeEnd,
	}

	srv := server.New(database, srvConfig)

	// Start control socket for management commands
	go startControlSocket(srv)

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		srv.Shutdown("maintenance", time.Time{})
		os.Remove(controlSocketPath)
		os.Exit(0)
	}()

	log.Fatal(srv.Start())
}

func startControlSocket(srv *server.Server) {
	// Remove existing socket file
	os.Remove(controlSocketPath)

	listener, err := net.Listen("unix", controlSocketPath)
	if err != nil {
		log.Printf("Failed to create control socket: %v", err)
		return
	}
	defer listener.Close()
	defer os.Remove(controlSocketPath)

	log.Printf("Control socket listening on %s", controlSocketPath)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go handleControlCommand(srv, conn)
	}
}

func handleControlCommand(srv *server.Server, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, "|", 3)

	if len(parts) == 0 {
		conn.Write([]byte("ERROR|Invalid command\n"))
		return
	}

	cmd := parts[0]

	switch cmd {
	case "stats":
		stats := srv.GetStats()
		conn.Write([]byte("OK|" + stats + "\n"))

	case "shutdown":
		reason := "maintenance"
		var completionTime time.Time

		if len(parts) >= 2 && parts[1] != "" {
			reason = parts[1]
		}
		if len(parts) >= 3 && parts[2] != "" {
			completionTime, _ = time.Parse(time.RFC3339, parts[2])
		}

		conn.Write([]byte("OK|Shutting down\n"))
		conn.Close()

		// Give time for response to be sent
		time.Sleep(100 * time.Millisecond)

		log.Printf("Shutdown requested: reason=%s, completion=%v", reason, completionTime)
		srv.Shutdown(reason, completionTime)

		os.Remove(controlSocketPath)
		os.Exit(0)

	default:
		conn.Write([]byte("ERROR|Unknown command\n"))
	}
}
