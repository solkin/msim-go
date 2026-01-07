package server

import (
	"bufio"
	"io"
	"log"
	"msim/db"
	"msim/protocol"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	db          *db.DB
	config      *ServerConfig
	sessions    map[string]*Session
	mu          sync.RWMutex
	fileManager *FileTransferManager
}

type ServerConfig struct {
	Port              int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	FilePortRangeStart int
	FilePortRangeEnd   int
}

type Session struct {
	Login    string
	Conn     net.Conn
	LastPing time.Time
	mu       sync.Mutex
}

func New(database *db.DB, config *ServerConfig) *Server {
	// Если диапазон портов не задан, используем значения по умолчанию
	if config.FilePortRangeStart == 0 {
		config.FilePortRangeStart = 35000
	}
	if config.FilePortRangeEnd == 0 {
		config.FilePortRangeEnd = 35999
	}

	fileManager := NewFileTransferManager(config.FilePortRangeStart, config.FilePortRangeEnd)
	fileManager.StartCleanupTask()

	return &Server{
		db:          database,
		config:      config,
		sessions:    make(map[string]*Session),
		fileManager: fileManager,
	}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(s.config.Port))
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("MSIM server started on port %d", s.config.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	shouldSendBye := false
	byeReason := ""
	byeDetails := ""

	defer func() {
		if shouldSendBye {
			s.sendBye(conn, byeReason, byeDetails)
		}
		conn.Close()
	}()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("New client connected from %s", remoteAddr)

	session := &Session{
		Conn:     conn,
		LastPing: time.Now(),
	}

	reader := bufio.NewReader(conn)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			s.mu.RLock()
			sess := session
			s.mu.RUnlock()

			if sess.Login != "" {
				s.mu.Lock()
				if sess, ok := s.sessions[sess.Login]; ok {
					if time.Since(sess.LastPing) > s.config.ReadTimeout {
						login := sess.Login
						s.mu.Unlock()
						// Устанавливаем флаг для отправки bye с причиной timeout
						shouldSendBye = true
						byeReason = "timeout"
						byeDetails = ""
						log.Printf("Client %s disconnected due to timeout from %s", login, remoteAddr)
						conn.Close()
						return
					}
				}
				s.mu.Unlock()
			}
		}
	}()

	for {
		conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				// Проверяем, не таймаут ли это
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Таймаут - это нормально, продолжаем ждать данные
					// Обновляем LastPing, чтобы соединение не закрылось
					session.mu.Lock()
					session.LastPing = time.Now()
					session.mu.Unlock()
					continue
				}
				// Проверяем, не закрыто ли соединение
				if strings.Contains(err.Error(), "use of closed network connection") {
					break
				}
				log.Printf("Error reading from %s: %v", remoteAddr, err)
			}
			// EOF или другая ошибка - закрываем соединение
			break
		}

		// Пропускаем пустые строки
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Логируем входящие пакеты (без паролей)
		if !strings.HasPrefix(line, "auth|") && !strings.HasPrefix(line, "reg|") {
			log.Printf("Received from %s: %q", remoteAddr, line)
		}

		pkt, err := protocol.ParsePacket(line + "\n")
		if err != nil {
			log.Printf("Parse error from %s: %v, line: %q", remoteAddr, err, line)
			s.sendError(conn, "", "Invalid packet format")
			continue
		}

		s.handlePacket(session, pkt, conn)

		// Если был отправлен bye от клиента, выходим из цикла
		// Сессия уже удалена в handleBye
		if pkt.Type == "bye" {
			return
		}
	}

	// Удаляем сессию при отключении (если не было bye)
	if session.Login != "" {
		s.removeSession(session.Login)

		// Обновляем время последнего отключения
		now := time.Now().UTC()
		if err := s.db.UpdateLastOffline(session.Login, now); err != nil {
			log.Printf("Failed to update last_offline for %s: %v", session.Login, err)
		}
		s.notifyContactsOffline(session.Login, now)
		log.Printf("Client %s disconnected from %s", session.Login, remoteAddr)
	} else {
		log.Printf("Client disconnected from %s", remoteAddr)
	}
}

func (s *Server) handlePacket(session *Session, pkt *protocol.Packet, conn net.Conn) {
	session.mu.Lock()
	session.LastPing = time.Now()
	session.mu.Unlock()

	switch pkt.Type {
	case "ping":
		s.handlePing(conn)
	case "auth":
		s.handleAuth(session, pkt, conn)
	case "reg":
		s.handleRegister(session, pkt, conn)
	case "msg":
		s.handleMessage(session, pkt, conn)
	case "ack":
		s.handleAck(session, pkt, conn)
	case "hist":
		s.handleHistory(session, pkt, conn)
	case "hclear":
		s.handleClearHistory(session, pkt, conn)
	case "offmsg":
		s.handleOfflineMessages(session, conn)
	case "stat":
		s.handleStatus(session, pkt, conn)
	case "list":
		s.handleList(session, conn)
	case "add":
		s.handleAddContact(session, pkt, conn)
	case "ren":
		s.handleRenameContact(session, pkt, conn)
	case "del":
		s.handleDeleteContact(session, pkt, conn)
	case "bye":
		s.handleBye(session, pkt, conn)
	case "help":
		s.handleHelp(conn)
	case "fsnd":
		s.handleFileSend(session, pkt, conn)
	case "facc":
		s.handleFileAccept(session, pkt, conn)
	case "fdec":
		s.handleFileDecline(session, pkt, conn)
	case "fcan":
		s.handleFileCancel(session, pkt, conn)
	case "fst":
		s.handleFileStatus(session, pkt, conn)
	default:
		s.sendError(conn, "", "Unknown packet type")
	}
}

// sendPacket отправляет пакет с несколькими полями, разделенными неэкранированным |
// Формат: pktType|field1|field2|...\n
// Каждое поле экранируется отдельно
// Используется для всех типов пакетов: TYPE, TYPE|CONTENT, TYPE|DESTINATION|CONTENT, и т.д.
func (s *Server) sendPacket(conn net.Conn, pktType string, fields ...string) {
	var parts []string
	parts = append(parts, protocol.Escape(pktType))

	for _, field := range fields {
		parts = append(parts, protocol.Escape(field))
	}

	packet := strings.Join(parts, "|") + "\n"
	conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
	if _, err := conn.Write([]byte(packet)); err != nil {
		log.Printf("Error writing to connection: %v", err)
	}
}

// sendPacketRaw отправляет пакет с неэкранированным content
// Используется для пакетов, где content содержит неэкранированные | (например, stat, list)
// Формат: pktType|rawContent\n
func (s *Server) sendPacketRaw(conn net.Conn, pktType, rawContent string) {
	packet := protocol.Escape(pktType) + "|" + rawContent + "\n"
	conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
	if _, err := conn.Write([]byte(packet)); err != nil {
		log.Printf("Error writing to connection: %v", err)
	}
}

func (s *Server) sendOK(conn net.Conn, operation string) {
	if operation != "" {
		s.sendPacket(conn, "ok", operation)
	} else {
		s.sendPacket(conn, "ok")
	}
}

func (s *Server) sendError(conn net.Conn, operation, description string) {
	if operation != "" {
		// Формат: fail|operation|description
		s.sendPacket(conn, "fail", operation, description)
	} else {
		s.sendPacket(conn, "fail", description)
	}
}

func (s *Server) sendBye(conn net.Conn, reason, details string) {
	if details != "" {
		// Формат: bye|reason|details
		s.sendPacket(conn, "bye", reason, details)
	} else {
		// Формат: bye|reason или просто bye
		if reason != "" {
			s.sendPacket(conn, "bye", reason)
		} else {
			s.sendPacket(conn, "bye")
		}
	}
}

func (s *Server) addSession(login string, session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[login] = session
}

func (s *Server) removeSession(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, login)
}

func (s *Server) getSession(login string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[login]
	return session, ok
}

func (s *Server) getSessionConn(login string) (net.Conn, bool) {
	session, ok := s.getSession(login)
	if !ok {
		return nil, false
	}
	return session.Conn, true
}

// GetStats returns server statistics as a formatted string
func (s *Server) GetStats() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeConnections := len(s.sessions)
	var users []string
	for login := range s.sessions {
		users = append(users, login)
	}

	return "connections=" + strconv.Itoa(activeConnections) + ",users=" + strings.Join(users, ";")
}
