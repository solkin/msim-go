package server

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// FileSession представляет сессию передачи файла
type FileSession struct {
	ID           string
	Sender       string
	Recipient    string
	Filename     string
	Size         int64
	Hash         string
	UploadPort   int
	DownloadPort int
	UploadConn   net.Conn
	DownloadConn net.Conn
	Status       string // "pending", "accepted", "transferring", "completed", "declined", "cancelled"
	CreatedAt    time.Time
	ExpiresAt    time.Time
	mu           sync.Mutex
}

// FileTransferManager управляет сессиями передачи файлов
type FileTransferManager struct {
	sessions       map[string]*FileSession
	mu             sync.RWMutex
	portRangeStart int
	portRangeEnd   int
	usedPorts      map[int]bool
	portMu         sync.Mutex
}

// NewFileTransferManager создает новый менеджер передачи файлов
func NewFileTransferManager(portStart, portEnd int) *FileTransferManager {
	return &FileTransferManager{
		sessions:       make(map[string]*FileSession),
		usedPorts:      make(map[int]bool),
		portRangeStart: portStart,
		portRangeEnd:   portEnd,
	}
}

// CreateSession создает новую файловую сессию
func (ftm *FileTransferManager) CreateSession(sender, recipient, filename string, size int64, hash string) (*FileSession, error) {
	sessionID := generateSessionID()

	session := &FileSession{
		ID:        sessionID,
		Sender:    sender,
		Recipient: recipient,
		Filename:  filename,
		Size:      size,
		Hash:      hash,
		Status:    "pending",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute), // 5 минут на акцепт
	}

	ftm.mu.Lock()
	ftm.sessions[sessionID] = session
	ftm.mu.Unlock()

	log.Printf("Created file session %s: %s -> %s, file: %s (%d bytes)", sessionID, sender, recipient, filename, size)
	return session, nil
}

// AcceptSession принимает файловую сессию и выделяет порты
func (ftm *FileTransferManager) AcceptSession(sessionID string) (uploadPort, downloadPort int, err error) {
	ftm.mu.Lock()
	session, exists := ftm.sessions[sessionID]
	ftm.mu.Unlock()

	if !exists {
		return 0, 0, ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Status != "pending" {
		return 0, 0, ErrSessionNotPending
	}

	// Выделяем два порта
	uploadPort, err = ftm.allocatePort()
	if err != nil {
		return 0, 0, err
	}

	downloadPort, err = ftm.allocatePort()
	if err != nil {
		ftm.releasePort(uploadPort)
		return 0, 0, err
	}

	session.UploadPort = uploadPort
	session.DownloadPort = downloadPort
	session.Status = "accepted"
	session.ExpiresAt = time.Now().Add(10 * time.Minute) // 10 минут на передачу

	// Запускаем прокси-серверы
	go ftm.startProxy(session, uploadPort, downloadPort)

	log.Printf("Accepted file session %s: upload port %d, download port %d", sessionID, uploadPort, downloadPort)
	return uploadPort, downloadPort, nil
}

// DeclineSession отклоняет файловую сессию
func (ftm *FileTransferManager) DeclineSession(sessionID string) error {
	ftm.mu.Lock()
	session, exists := ftm.sessions[sessionID]
	ftm.mu.Unlock()

	if !exists {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	session.Status = "declined"
	session.mu.Unlock()

	log.Printf("Declined file session %s", sessionID)
	return nil
}

// CancelSession отменяет файловую сессию
func (ftm *FileTransferManager) CancelSession(sessionID string) error {
	ftm.mu.Lock()
	session, exists := ftm.sessions[sessionID]
	ftm.mu.Unlock()

	if !exists {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	session.Status = "cancelled"
	// Закрываем соединения если они есть
	if session.UploadConn != nil {
		session.UploadConn.Close()
	}
	if session.DownloadConn != nil {
		session.DownloadConn.Close()
	}
	session.mu.Unlock()

	log.Printf("Cancelled file session %s", sessionID)
	return nil
}

// GetSession возвращает сессию по ID
func (ftm *FileTransferManager) GetSession(sessionID string) (*FileSession, bool) {
	ftm.mu.RLock()
	defer ftm.mu.RUnlock()
	session, exists := ftm.sessions[sessionID]
	return session, exists
}

// CleanExpired очищает устаревшие сессии
func (ftm *FileTransferManager) CleanExpired() {
	ftm.mu.Lock()
	defer ftm.mu.Unlock()

	now := time.Now()
	for id, session := range ftm.sessions {
		session.mu.Lock()
		if now.After(session.ExpiresAt) && session.Status != "completed" {
			log.Printf("Cleaning expired file session %s", id)
			session.Status = "cancelled"
			if session.UploadConn != nil {
				session.UploadConn.Close()
			}
			if session.DownloadConn != nil {
				session.DownloadConn.Close()
			}
			if session.UploadPort > 0 {
				ftm.releasePort(session.UploadPort)
			}
			if session.DownloadPort > 0 {
				ftm.releasePort(session.DownloadPort)
			}
			delete(ftm.sessions, id)
		}
		session.mu.Unlock()
	}
}

// StartCleanupTask запускает фоновую задачу очистки
func (ftm *FileTransferManager) StartCleanupTask() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			ftm.CleanExpired()
		}
	}()
}

// allocatePort выделяет свободный порт из диапазона
func (ftm *FileTransferManager) allocatePort() (int, error) {
	ftm.portMu.Lock()
	defer ftm.portMu.Unlock()

	for port := ftm.portRangeStart; port <= ftm.portRangeEnd; port++ {
		if !ftm.usedPorts[port] {
			ftm.usedPorts[port] = true
			return port, nil
		}
	}

	return 0, ErrNoAvailablePorts
}

// releasePort освобождает порт
func (ftm *FileTransferManager) releasePort(port int) {
	ftm.portMu.Lock()
	defer ftm.portMu.Unlock()
	delete(ftm.usedPorts, port)
}

// startProxy запускает TCP прокси для передачи файла
func (ftm *FileTransferManager) startProxy(session *FileSession, uploadPort, downloadPort int) {
	// Запускаем листенеры на обоих портах
	uploadListener, err := net.Listen("tcp", ":"+itoa(uploadPort))
	if err != nil {
		log.Printf("Failed to start upload listener on port %d: %v", uploadPort, err)
		return
	}
	defer uploadListener.Close()

	downloadListener, err := net.Listen("tcp", ":"+itoa(downloadPort))
	if err != nil {
		log.Printf("Failed to start download listener on port %d: %v", downloadPort, err)
		return
	}
	defer downloadListener.Close()

	log.Printf("File transfer proxy started for session %s: upload=%d, download=%d", session.ID, uploadPort, downloadPort)

	// Каналы для синхронизации
	uploadReady := make(chan net.Conn, 1)
	downloadReady := make(chan net.Conn, 1)
	done := make(chan struct{})

	// Горутина для приема upload соединения
	go func() {
		conn, err := uploadListener.Accept()
		if err != nil {
			log.Printf("Upload accept error for session %s: %v", session.ID, err)
			close(done)
			return
		}
		log.Printf("Upload connection established for session %s", session.ID)
		uploadReady <- conn
	}()

	// Горутина для приема download соединения
	go func() {
		conn, err := downloadListener.Accept()
		if err != nil {
			log.Printf("Download accept error for session %s: %v", session.ID, err)
			close(done)
			return
		}
		log.Printf("Download connection established for session %s", session.ID)
		downloadReady <- conn
	}()

	// Ждем оба соединения или таймаут
	var uploadConn, downloadConn net.Conn
	timeout := time.After(session.ExpiresAt.Sub(time.Now()))

	for uploadConn == nil || downloadConn == nil {
		select {
		case uploadConn = <-uploadReady:
		case downloadConn = <-downloadReady:
		case <-timeout:
			log.Printf("Timeout waiting for connections for session %s", session.ID)
			if uploadConn != nil {
				uploadConn.Close()
			}
			if downloadConn != nil {
				downloadConn.Close()
			}
			ftm.releasePort(uploadPort)
			ftm.releasePort(downloadPort)
			return
		case <-done:
			if uploadConn != nil {
				uploadConn.Close()
			}
			if downloadConn != nil {
				downloadConn.Close()
			}
			ftm.releasePort(uploadPort)
			ftm.releasePort(downloadPort)
			return
		}
	}

	session.mu.Lock()
	session.UploadConn = uploadConn
	session.DownloadConn = downloadConn
	session.Status = "transferring"
	session.mu.Unlock()

	log.Printf("Starting file transfer for session %s", session.ID)

	// Пробрасываем данные от upload к download
	bytesTransferred, err := io.Copy(downloadConn, uploadConn)
	if err != nil {
		log.Printf("File transfer error for session %s: %v", session.ID, err)
	} else {
		log.Printf("File transfer completed for session %s: %d bytes transferred", session.ID, bytesTransferred)
	}

	// Закрываем соединения
	uploadConn.Close()
	downloadConn.Close()

	// Обновляем статус
	session.mu.Lock()
	if err == nil {
		session.Status = "completed"
	} else {
		session.Status = "error"
	}
	session.mu.Unlock()

	// Освобождаем порты
	ftm.releasePort(uploadPort)
	ftm.releasePort(downloadPort)

	log.Printf("File transfer session %s finished with status: %s", session.ID, session.Status)
}

// generateSessionID генерирует уникальный ID сессии
func generateSessionID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// itoa преобразует int в string (быстрая версия для положительных чисел)
func itoa(n int) string {
	if n < 10 {
		return string('0' + byte(n))
	}
	var buf [20]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = '0' + byte(n%10)
		n /= 10
		i--
	}
	return string(buf[i+1:])
}

// Ошибки
var (
	ErrSessionNotFound   = &FileTransferError{msg: "session not found"}
	ErrSessionNotPending = &FileTransferError{msg: "session not in pending state"}
	ErrNoAvailablePorts  = &FileTransferError{msg: "no available ports"}
)

type FileTransferError struct {
	msg string
}

func (e *FileTransferError) Error() string {
	return e.msg
}

