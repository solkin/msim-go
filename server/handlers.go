package server

import (
	"log"
	"msim/db"
	"msim/protocol"
	"net"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handlePing(conn net.Conn) {
	s.sendPacket(conn, "pong")
}

func (s *Server) handleAuth(session *Session, pkt *protocol.Packet, conn net.Conn) {
	var login, password string

	// Формат: auth|login|password (DESTINATION=login, CONTENT=password)
	// или auth|login|password (все в CONTENT)
	if pkt.Destination != "" {
		login = pkt.Destination
		password = pkt.Content
	} else {
		if len(pkt.Fields) < 2 {
			s.sendError(conn, "auth", "Invalid credentials")
			return
		}
		login = pkt.Fields[0]
		password = pkt.Fields[1]
	}

	if login == "" || password == "" {
		s.sendError(conn, "auth", "Invalid credentials")
		return
	}

	// Если уже авторизован
	if session.Login != "" {
		s.sendOK(conn, "auth")
		return
	}

	valid, err := s.db.AuthenticateUser(login, password)
	if err != nil {
		log.Printf("Auth error: %v", err)
		s.sendError(conn, "auth", "Internal error")
		return
	}

	if !valid {
		s.sendError(conn, "auth", "Invalid credentials")
		return
	}

	// Авторизация успешна
	session.Login = login
	s.addSession(login, session)
	s.sendOK(conn, "auth")

	// Обновляем время последнего подключения
	now := time.Now().UTC()
	if err := s.db.UpdateLastOnline(login, now); err != nil {
		log.Printf("Failed to update last_online for %s: %v", login, err)
	}
	s.notifyContactsOnline(login, now)
}

func (s *Server) handleRegister(session *Session, pkt *protocol.Packet, conn net.Conn) {
	var login, password string

	// Формат: reg|login|password (DESTINATION=login, CONTENT=password)
	// или reg|login|password (все в CONTENT)
	if pkt.Destination != "" {
		login = pkt.Destination
		password = pkt.Content
	} else {
		if len(pkt.Fields) < 2 {
			s.sendError(conn, "reg", "Invalid data")
			return
		}
		login = pkt.Fields[0]
		password = pkt.Fields[1]
	}

	if login == "" || password == "" {
		s.sendError(conn, "reg", "Invalid data")
		return
	}

	exists, err := s.db.UserExists(login)
	if err != nil {
		log.Printf("Register error: %v", err)
		s.sendError(conn, "reg", "Internal error")
		return
	}

	if exists {
		s.sendError(conn, "reg", "User already exists")
		return
	}

	err = s.db.CreateUser(login, password)
	if err != nil {
		log.Printf("Register error: %v", err)
		s.sendError(conn, "reg", "Internal error")
		return
	}

	s.sendOK(conn, "reg")
}

func (s *Server) handleMessage(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "msg", "Not authenticated")
		return
	}

	recipient := pkt.Destination
	text := pkt.Content

	if recipient == "" {
		s.sendError(conn, "msg", "Recipient required")
		return
	}

	if text == "" {
		s.sendError(conn, "msg", "Message text required")
		return
	}

	// Проверяем, существует ли получатель в системе
	exists, err := s.db.UserExists(recipient)
	if err != nil {
		log.Printf("Message error: %v", err)
		s.sendError(conn, "msg", "Internal error")
		return
	}

	if !exists {
		s.sendError(conn, "msg", "Recipient not found")
		return
	}

	timestamp := time.Now().UTC()
	err = s.db.SaveMessage(session.Login, recipient, text, timestamp)
	if err != nil {
		log.Printf("Message error: %v", err)
		s.sendError(conn, "msg", "Internal error")
		return
	}

	// Отправляем сообщение получателю, если он онлайн
	// Формат: msg|sender|text|timestamp (timestamp - отдельное неэкранированное поле)
	if recipientConn, ok := s.getSessionConn(recipient); ok {
		timestampStr := timestamp.Format("2006-01-02T15:04:05Z")
		// Формируем пакет вручную: msg|sender|text|timestamp
		packet := "msg|" + protocol.Escape(session.Login) + "|" + protocol.Escape(text) + "|" + timestampStr + "\n"
		recipientConn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
		if _, err := recipientConn.Write([]byte(packet)); err != nil {
			log.Printf("Error writing message to %s: %v", recipient, err)
		}
	}

	s.sendOK(conn, "msg")
}

func (s *Server) handleAck(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "ack", "Not authenticated")
		return
	}

	var sender, timestampStr string

	// Формат: ack|sender|timestamp (DESTINATION=sender, CONTENT=timestamp)
	if pkt.Destination != "" {
		sender = pkt.Destination
		timestampStr = pkt.Content
	} else {
		if len(pkt.Fields) < 2 {
			s.sendError(conn, "ack", "Invalid ack format")
			return
		}
		sender = pkt.Fields[0]
		timestampStr = pkt.Fields[1]
	}

	if sender == "" || timestampStr == "" {
		s.sendError(conn, "ack", "Invalid ack format")
		return
	}

	timestamp, err := time.Parse("2006-01-02T15:04:05Z", timestampStr)
	if err != nil {
		s.sendError(conn, "ack", "Invalid timestamp")
		return
	}

	// Обновляем статус сообщения
	err = s.db.MarkMessageAcknowledged(sender, session.Login, timestamp)
	if err != nil {
		log.Printf("Ack error: %v", err)
		s.sendError(conn, "ack", "Internal error")
		return
	}

	// Отправляем подтверждение отправителю ack
	s.sendOK(conn, "ack")

	// Пересылаем подтверждение отправителю сообщения
	if senderConn, ok := s.getSessionConn(sender); ok {
		s.sendPacket(senderConn, "ack", session.Login, timestampStr)
	}
}

func (s *Server) handleHistory(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "hist", "Not authenticated")
		return
	}

	contact := pkt.Destination
	if contact == "" {
		if len(pkt.Fields) > 0 {
			contact = pkt.Fields[0]
		} else {
			s.sendError(conn, "hist", "Contact required")
			return
		}
	}

	offset := 0
	limit := 1000 // по умолчанию большое число для получения всех сообщений

	// Формат: hist|contact или hist|contact|limit или hist|contact|offset|limit
	// Если DESTINATION указан, то параметры в Fields (из CONTENT)
	if pkt.Destination != "" {
		// hist|contact|limit - один параметр в CONTENT
		if len(pkt.Fields) == 1 {
			if parsed, err := strconv.Atoi(pkt.Fields[0]); err == nil {
				limit = parsed
			}
		}
		// hist|contact|offset|limit - два параметра в CONTENT
		if len(pkt.Fields) >= 2 {
			if parsed, err := strconv.Atoi(pkt.Fields[0]); err == nil {
				offset = parsed
			}
			if parsed, err := strconv.Atoi(pkt.Fields[1]); err == nil {
				limit = parsed
			}
		}
	} else {
		// Формат без DESTINATION: hist|contact|limit или hist|contact|offset|limit (все в CONTENT)
		if len(pkt.Fields) >= 1 {
			contact = pkt.Fields[0]
		}
		if len(pkt.Fields) >= 2 {
			if parsed, err := strconv.Atoi(pkt.Fields[1]); err == nil {
				limit = parsed
			}
		}
		if len(pkt.Fields) >= 3 {
			if parsed, err := strconv.Atoi(pkt.Fields[1]); err == nil {
				offset = parsed
			}
			if parsed, err := strconv.Atoi(pkt.Fields[2]); err == nil {
				limit = parsed
			}
		}
	}

	messages, err := s.db.GetMessages(session.Login, contact, offset, limit)
	if err != nil {
		log.Printf("History error: %v", err)
		s.sendError(conn, "hist", "Internal error")
		return
	}

	var items []string
	for _, msg := range messages {
		timestampStr := msg.Timestamp.Format("2006-01-02T15:04:05Z")
		// Формат: msg|sender|text|timestamp|status (| не экранируются внутри списка)
		item := "msg|" + protocol.Escape(msg.Sender) + "|" + protocol.Escape(msg.Text) + "|" + timestampStr + "|" + msg.Status
		items = append(items, item)
	}

	response := strings.Join(items, ",")
	// Формат: hist|contact|msg|sender|text|timestamp|status,msg|...
	// response содержит msg|sender|text|timestamp|status, где | не должны экранироваться
	rawContent := protocol.Escape(contact) + "|" + response
	s.sendPacketRaw(conn, "hist", rawContent)
}

func (s *Server) handleClearHistory(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "hclear", "Not authenticated")
		return
	}

	contact := pkt.Destination
	if contact == "" {
		if len(pkt.Fields) > 0 {
			contact = pkt.Fields[0]
		} else {
			contact = pkt.Content
		}
	}

	if contact == "" {
		s.sendError(conn, "hclear", "Contact required")
		return
	}

	err := s.db.ClearHistory(session.Login, contact)
	if err != nil {
		log.Printf("Clear history error: %v", err)
		s.sendError(conn, "hclear", "Internal error")
		return
	}

	s.sendOK(conn, "hclear")
}

func (s *Server) handleStatus(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "stat", "Not authenticated")
		return
	}

	// Извлекаем опциональный параметр - ID пользователя
	var targetUser string
	if pkt.Destination != "" {
		targetUser = pkt.Destination
	} else if pkt.Content != "" {
		targetUser = pkt.Content
	} else if len(pkt.Fields) > 0 {
		targetUser = pkt.Fields[0]
	}

	var items []string

	if targetUser != "" {
		// Запрос статуса конкретного пользователя
		// Проверяем, существует ли пользователь
		exists, err := s.db.UserExists(targetUser)
		if err != nil {
			log.Printf("Status error: %v", err)
			s.sendError(conn, "stat", "Internal error")
			return
		}

		if !exists {
			s.sendError(conn, "stat", "User not found")
			return
		}

		// Проверяем статус пользователя и получаем last_seen
		status := "off"
		var lastSeen time.Time
		if _, ok := s.getSession(targetUser); ok {
			status = "on"
		}

		// Получаем время последнего изменения статуса
		lastOnline, lastOffline, err := s.db.GetUserStatus(targetUser)
		if err != nil {
			log.Printf("Status error getting user status: %v", err)
			s.sendError(conn, "stat", "Internal error")
			return
		}

		// last_seen - большее из last_online и last_offline
		if lastOnline.After(lastOffline) {
			lastSeen = lastOnline
		} else {
			lastSeen = lastOffline
		}

		// Формат: user|status|last_seen (| не экранируется внутри списка)
		item := protocol.Escape(targetUser) + "|" + status + "|" + lastSeen.Format(time.RFC3339)
		items = append(items, item)
	} else {
		// Запрос статусов всех контактов
		contacts, err := s.db.GetContacts(session.Login)
		if err != nil {
			log.Printf("Status error: %v", err)
			s.sendError(conn, "stat", "Internal error")
			return
		}

		for _, contact := range contacts {
			status := "off"
			var lastSeen time.Time
			if _, ok := s.getSession(contact.Contact); ok {
				status = "on"
			}

			// Получаем время последнего изменения статуса
			lastOnline, lastOffline, err := s.db.GetUserStatus(contact.Contact)
			if err != nil {
				log.Printf("Status error getting contact status: %v", err)
				continue // Пропускаем контакт при ошибке
			}

			// last_seen - большее из last_online и last_offline
			if lastOnline.After(lastOffline) {
				lastSeen = lastOnline
			} else {
				lastSeen = lastOffline
			}

			// Формат: user|status|last_seen (| не экранируется внутри списка)
			item := protocol.Escape(contact.Contact) + "|" + status + "|" + lastSeen.Format(time.RFC3339)
			items = append(items, item)
		}
	}

	response := strings.Join(items, ",")
	// response содержит user|status|last_seen, где | не должен экранироваться
	s.sendPacketRaw(conn, "stat", response)
}

func (s *Server) handleList(session *Session, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "list", "Not authenticated")
		return
	}

	contacts, err := s.db.GetContacts(session.Login)
	if err != nil {
		log.Printf("List error: %v", err)
		s.sendError(conn, "list", "Internal error")
		return
	}

	var items []string
	for _, contact := range contacts {
		// Формат: contact|nick (| не экранируется внутри списка)
		item := protocol.Escape(contact.Contact) + "|" + protocol.Escape(contact.Nick)
		items = append(items, item)
	}

	response := strings.Join(items, ",")
	// response содержит contact|nick, где | не должен экранироваться
	s.sendPacketRaw(conn, "list", response)
}

func (s *Server) handleAddContact(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "add", "Not authenticated")
		return
	}

	// Формат: add|contact|nick или add|contact (DESTINATION=contact, CONTENT=nick или пусто)
	// или add|contact|nick где оба в CONTENT
	var contact, nick string
	if pkt.Destination != "" {
		contact = pkt.Destination
		if len(pkt.Fields) > 0 {
			nick = pkt.Fields[0]
		} else {
			nick = pkt.Content
		}
	} else {
		if len(pkt.Fields) < 1 {
			s.sendError(conn, "add", "Invalid data")
			return
		}
		contact = pkt.Fields[0]
		if len(pkt.Fields) >= 2 {
			nick = pkt.Fields[1]
		}
	}

	if contact == "" {
		s.sendError(conn, "add", "Invalid data")
		return
	}

	// Проверяем, существует ли пользователь-контакт в системе
	exists, err := s.db.UserExists(contact)
	if err != nil {
		log.Printf("Add contact error: %v", err)
		s.sendError(conn, "add", "Internal error")
		return
	}

	if !exists {
		s.sendError(conn, "add", "User not found")
		return
	}

	// Если ник не указан, используем id контакта в качестве ника
	if nick == "" {
		nick = contact
	}

	err = s.db.AddContact(session.Login, contact, nick)
	if err != nil {
		log.Printf("Add contact error: %v", err)
		s.sendError(conn, "add", "Contact already exists or internal error")
		return
	}

	s.sendOK(conn, "add")
}

func (s *Server) handleRenameContact(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "ren", "Not authenticated")
		return
	}

	var contact, nick string
	if pkt.Destination != "" {
		contact = pkt.Destination
		if len(pkt.Fields) > 0 {
			nick = pkt.Fields[0]
		} else {
			nick = pkt.Content
		}
	} else {
		if len(pkt.Fields) < 2 {
			s.sendError(conn, "ren", "Invalid data")
			return
		}
		contact = pkt.Fields[0]
		nick = pkt.Fields[1]
	}

	if contact == "" || nick == "" {
		s.sendError(conn, "ren", "Invalid data")
		return
	}

	err := s.db.UpdateContactNick(session.Login, contact, nick)
	if err != nil {
		if err == db.ErrNoRows {
			s.sendError(conn, "ren", "Contact not found")
		} else {
			log.Printf("Rename contact error: %v", err)
			s.sendError(conn, "ren", "Internal error")
		}
		return
	}

	s.sendOK(conn, "ren")
}

func (s *Server) handleDeleteContact(session *Session, pkt *protocol.Packet, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "del", "Not authenticated")
		return
	}

	var contact string
	if pkt.Destination != "" {
		contact = pkt.Destination
	} else {
		if len(pkt.Fields) > 0 {
			contact = pkt.Fields[0]
		} else {
			contact = pkt.Content
		}
	}

	if contact == "" {
		s.sendError(conn, "del", "Invalid data")
		return
	}

	err := s.db.DeleteContact(session.Login, contact)
	if err != nil {
		if err == db.ErrNoRows {
			s.sendError(conn, "del", "Contact not found")
		} else {
			log.Printf("Delete contact error: %v", err)
			s.sendError(conn, "del", "Internal error")
		}
		return
	}

	s.sendOK(conn, "del")
}

func (s *Server) notifyContactsOnline(login string, timestamp time.Time) {
	contacts, err := s.db.GetContacts(login)
	if err != nil {
		return
	}

	ts := timestamp.Format(time.RFC3339)
	for _, contact := range contacts {
		if conn, ok := s.getSessionConn(contact.Contact); ok {
			s.sendPacket(conn, "on", login, ts)
		}
	}
}

func (s *Server) notifyContactsOffline(login string, timestamp time.Time) {
	contacts, err := s.db.GetContacts(login)
	if err != nil {
		return
	}

	ts := timestamp.Format(time.RFC3339)
	for _, contact := range contacts {
		if conn, ok := s.getSessionConn(contact.Contact); ok {
			s.sendPacket(conn, "off", login, ts)
		}
	}
}

func (s *Server) handleBye(session *Session, pkt *protocol.Packet, conn net.Conn) {
	// Клиент запросил завершение сессии
	// Отправляем подтверждение
	s.sendPacket(conn, "bye")

	// Удаляем сессию
	if session.Login != "" {
		remoteAddr := conn.RemoteAddr().String()
		s.removeSession(session.Login)

		// Обновляем время последнего отключения
		now := time.Now().UTC()
		if err := s.db.UpdateLastOffline(session.Login, now); err != nil {
			log.Printf("Failed to update last_offline for %s: %v", session.Login, err)
		}
		s.notifyContactsOffline(session.Login, now)
		log.Printf("Client %s disconnected (bye) from %s", session.Login, remoteAddr)
	}

	// Соединение закроется в defer handleConnection
}

// Shutdown отправляет bye всем подключенным клиентам с указанием причины
// reason может быть: "maintenance", "restart", "timeout"
// completionTime - время завершения обслуживания/перезагрузки в формате ISO 8601 (UTC)
// Для timeout completionTime может быть нулевым
func (s *Server) Shutdown(reason string, completionTime time.Time) {
	s.mu.RLock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()

	var details string
	if !completionTime.IsZero() {
		details = completionTime.UTC().Format("2006-01-02T15:04:05Z")
	}

	now := time.Now().UTC()
	for _, sess := range sessions {
		s.sendBye(sess.Conn, reason, details)
		sess.Conn.Close()
		if sess.Login != "" {
			// Обновляем время последнего отключения
			if err := s.db.UpdateLastOffline(sess.Login, now); err != nil {
				log.Printf("Failed to update last_offline for %s: %v", sess.Login, err)
			}
			s.removeSession(sess.Login)
		}
	}
}

func (s *Server) handleHelp(conn net.Conn) {
	// Формат: help|command1,command2,command3,...
	commands := []string{
		"ping",
		"auth",
		"reg",
		"msg",
		"ack",
		"hist",
		"hclear",
		"offmsg",
		"stat",
		"list",
		"add",
		"ren",
		"del",
		"bye",
		"help",
	}

	response := strings.Join(commands, ",")
	// response содержит список команд через запятую, запятые не должны экранироваться
	s.sendPacketRaw(conn, "help", response)
}

func (s *Server) handleOfflineMessages(session *Session, conn net.Conn) {
	if session.Login == "" {
		s.sendError(conn, "offmsg", "Not authenticated")
		return
	}

	counts, err := s.db.GetOfflineMessageCounts(session.Login)
	if err != nil {
		log.Printf("Offmsg error: %v", err)
		s.sendError(conn, "offmsg", "Internal error")
		return
	}

	var items []string
	for sender, count := range counts {
		// Формат: contact|count (| не экранируется внутри списка)
		item := protocol.Escape(sender) + "|" + strconv.Itoa(count)
		items = append(items, item)
	}

	response := strings.Join(items, ",")
	// response содержит contact|count, где | не должен экранироваться
	s.sendPacketRaw(conn, "offmsg", response)
}
