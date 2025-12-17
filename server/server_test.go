package server

import (
	"bufio"
	"msim/db"
	"msim/protocol"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// setupTestServer создает тестовый сервер с временной базой данных
func setupTestServer(t *testing.T) (*Server, func()) {
	// Создаем временный файл для базы данных
	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()
	os.Remove(tmpfile.Name()) // Удаляем файл, SQLite создаст его заново

	// Создаем базу данных
	database, err := db.New(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Создаем конфигурацию сервера
	config := &ServerConfig{
		Port:         0, // 0 означает автоматический выбор порта
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Создаем сервер
	srv := New(database, config)

	// Функция очистки
	cleanup := func() {
		database.Close()
		os.Remove(tmpfile.Name())
	}

	return srv, cleanup
}

// createTestConnection создает тестовое соединение для симуляции клиента
func createTestConnection() (net.Conn, net.Conn) {
	serverConn, clientConn := net.Pipe()
	return serverConn, clientConn
}

// readResponse читает ответ от сервера
func readResponse(conn net.Conn, timeout time.Duration) (string, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

// sendRequest отправляет запрос на сервер
func sendRequest(conn net.Conn, request string) error {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write([]byte(request + "\n"))
	return err
}

// TestPing тестирует команду ping
func TestPing(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	// Обрабатываем соединение в отдельной горутине
	go func() {
		srv.handleConnection(serverConn)
	}()

	// Отправляем ping
	err := sendRequest(clientConn, "ping")
	if err != nil {
		t.Fatalf("Failed to send ping: %v", err)
	}

	// Читаем ответ
	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "pong"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestRegister тестирует команду reg
func TestRegister(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Регистрация нового пользователя
	err := sendRequest(clientConn, "reg|testuser@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send reg: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|reg"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Попытка зарегистрировать того же пользователя должна вернуть ошибку
	err = sendRequest(clientConn, "reg|testuser@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send reg: %v", err)
	}

	response, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "fail|reg|") {
		t.Errorf("Expected fail|reg|..., got %q", response)
	}
}

// TestAuth тестирует команду auth
func TestAuth(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Сначала регистрируем пользователя
	err := srv.db.CreateUser("testuser@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Успешная авторизация
	err = sendRequest(clientConn, "auth|testuser@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|auth"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Неуспешная авторизация с неверным паролем
	serverConn2, clientConn2 := createTestConnection()
	defer serverConn2.Close()
	defer clientConn2.Close()

	go func() {
		srv.handleConnection(serverConn2)
	}()

	err = sendRequest(clientConn2, "auth|testuser@example.com|wrongpassword")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}

	response, err = readResponse(clientConn2, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "fail|auth|") {
		t.Errorf("Expected fail|auth|..., got %q", response)
	}
}

// TestMessage тестирует команду msg
func TestMessage(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем двух пользователей
	err := srv.db.CreateUser("sender@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("recipient@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Создаем соединения для отправителя и получателя
	senderServerConn, senderClientConn := createTestConnection()
	recipientServerConn, recipientClientConn := createTestConnection()
	defer senderServerConn.Close()
	defer senderClientConn.Close()
	defer recipientServerConn.Close()
	defer recipientClientConn.Close()

	// Обрабатываем соединения
	go func() {
		srv.handleConnection(senderServerConn)
	}()
	go func() {
		srv.handleConnection(recipientServerConn)
	}()

	// Авторизуем отправителя
	err = sendRequest(senderClientConn, "auth|sender@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Авторизуем получателя
	err = sendRequest(recipientClientConn, "auth|recipient@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Отправляем сообщение
	err = sendRequest(senderClientConn, "msg|recipient@example.com|Hello, World!")
	if err != nil {
		t.Fatalf("Failed to send msg: %v", err)
	}

	// Проверяем, что получатель получил сообщение (может прийти раньше ответа отправителю)
	msgResponse, err := readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	// Запятая в сообщении должна быть экранирована
	if !strings.Contains(msgResponse, "msg|sender@example.com|") {
		t.Errorf("Expected msg|sender@example.com|... in response, got %q", msgResponse)
	}
	if !strings.Contains(msgResponse, "Hello") || !strings.Contains(msgResponse, "World") {
		t.Errorf("Expected message to contain 'Hello' and 'World', got %q", msgResponse)
	}

	// Проверяем ответ отправителю
	response, err := readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|msg"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestAck тестирует команду ack
func TestAck(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем двух пользователей
	err := srv.db.CreateUser("sender@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("recipient@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Создаем соединения
	senderServerConn, senderClientConn := createTestConnection()
	recipientServerConn, recipientClientConn := createTestConnection()
	defer senderServerConn.Close()
	defer senderClientConn.Close()
	defer recipientServerConn.Close()
	defer recipientClientConn.Close()

	go func() {
		srv.handleConnection(senderServerConn)
	}()
	go func() {
		srv.handleConnection(recipientServerConn)
	}()

	// Авторизуем обоих
	err = sendRequest(senderClientConn, "auth|sender@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	err = sendRequest(recipientClientConn, "auth|recipient@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Отправляем сообщение
	err = sendRequest(senderClientConn, "msg|recipient@example.com|Test message")
	if err != nil {
		t.Fatalf("Failed to send msg: %v", err)
	}

	// Получаем сообщение и извлекаем timestamp
	msgResponse, err := readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	// Извлекаем timestamp напрямую из строки
	// Формат: msg|sender|text|timestamp
	parts := strings.Split(msgResponse, "|")
	if len(parts) < 4 {
		t.Fatalf("Invalid message format: %q", msgResponse)
	}
	timestamp := parts[3]

	// Сначала читаем ok|msg отправителю
	okMsg, err := readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read ok|msg: %v", err)
	}
	if okMsg != "ok|msg" {
		t.Errorf("Expected ok|msg, got %q", okMsg)
	}

	// Отправляем ack
	err = sendRequest(recipientClientConn, "ack|sender@example.com|"+timestamp)
	if err != nil {
		t.Fatalf("Failed to send ack: %v", err)
	}

	// Проверяем ответ получателю
	ackResponse, err := readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read ack response: %v", err)
	}

	expected := "ok|ack"
	if ackResponse != expected {
		t.Errorf("Expected %q, got %q", expected, ackResponse)
	}

	// Проверяем, что отправитель получил ack
	ackToSender, err := readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read ack to sender: %v", err)
	}

	if !strings.HasPrefix(ackToSender, "ack|recipient@example.com|") {
		t.Errorf("Expected ack|recipient@example.com|..., got %q", ackToSender)
	}
}

// TestHistory тестирует команду hist
func TestHistory(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Сохраняем сообщения в базу
	timestamp1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	timestamp2 := time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC)

	err = srv.db.SaveMessage("user1@example.com", "user2@example.com", "Hello", timestamp1)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}
	err = srv.db.SaveMessage("user2@example.com", "user1@example.com", "Hi there", timestamp2)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Запрашиваем историю
	err = sendRequest(clientConn, "hist|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send hist: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "hist|user2@example.com|") {
		t.Errorf("Expected hist|user2@example.com|..., got %q", response)
	}

	// Проверяем, что в истории есть оба сообщения
	if !strings.Contains(response, "Hello") || !strings.Contains(response, "Hi there") {
		t.Errorf("History should contain both messages, got %q", response)
	}
}

// TestClearHistory тестирует команду hclear
func TestClearHistory(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Сохраняем сообщение
	timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	err = srv.db.SaveMessage("user1@example.com", "user2@example.com", "Test", timestamp)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Очищаем историю
	err = sendRequest(clientConn, "hclear|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send hclear: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|hclear"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Проверяем, что история пуста
	err = sendRequest(clientConn, "hist|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send hist: %v", err)
	}

	histResponse, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read hist response: %v", err)
	}

	// История должна быть пустой (только заголовок)
	if !strings.HasPrefix(histResponse, "hist|user2@example.com|") {
		t.Errorf("Expected hist|user2@example.com|..., got %q", histResponse)
	}
}

// TestStatus тестирует команду stat
func TestStatus(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user3@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Добавляем контакты
	err = srv.db.AddContact("user1@example.com", "user2@example.com", "User 2")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}
	err = srv.db.AddContact("user1@example.com", "user3@example.com", "User 3")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	// Создаем соединения
	user1ServerConn, user1ClientConn := createTestConnection()
	user2ServerConn, user2ClientConn := createTestConnection()
	defer user1ServerConn.Close()
	defer user1ClientConn.Close()
	defer user2ServerConn.Close()
	defer user2ClientConn.Close()

	go func() {
		srv.handleConnection(user1ServerConn)
	}()
	go func() {
		srv.handleConnection(user2ServerConn)
	}()

	// Авторизуем user1
	err = sendRequest(user1ClientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(user1ClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Авторизуем user2 (онлайн)
	err = sendRequest(user2ClientConn, "auth|user2@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(user2ClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Запрашиваем статусы всех контактов
	err = sendRequest(user1ClientConn, "stat")
	if err != nil {
		t.Fatalf("Failed to send stat: %v", err)
	}

	response, err := readResponse(user1ClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "stat|") {
		t.Errorf("Expected stat|..., got %q", response)
	}

	// Проверяем, что user2 онлайн
	if !strings.Contains(response, "user2@example.com|on") {
		t.Errorf("Expected user2@example.com|on in response, got %q", response)
	}

	// Проверяем, что user3 оффлайн
	if !strings.Contains(response, "user3@example.com|off") {
		t.Errorf("Expected user3@example.com|off in response, got %q", response)
	}

	// Тестируем запрос статуса конкретного пользователя
	err = sendRequest(user1ClientConn, "stat|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send stat: %v", err)
	}

	response, err = readResponse(user1ClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "stat|user2@example.com|on"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestList тестирует команду list
func TestList(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user3@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Добавляем контакты
	err = srv.db.AddContact("user1@example.com", "user2@example.com", "Friend 1")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}
	err = srv.db.AddContact("user1@example.com", "user3@example.com", "Friend 2")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Запрашиваем список контактов
	err = sendRequest(clientConn, "list")
	if err != nil {
		t.Fatalf("Failed to send list: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "list|") {
		t.Errorf("Expected list|..., got %q", response)
	}

	// Проверяем наличие контактов
	if !strings.Contains(response, "user2@example.com|Friend 1") {
		t.Errorf("Expected user2@example.com|Friend 1 in response, got %q", response)
	}
	if !strings.Contains(response, "user3@example.com|Friend 2") {
		t.Errorf("Expected user3@example.com|Friend 2 in response, got %q", response)
	}
}

// TestAddContact тестирует команду add
func TestAddContact(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Добавляем контакт с ником
	err = sendRequest(clientConn, "add|user2@example.com|My Friend")
	if err != nil {
		t.Fatalf("Failed to send add: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|add"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Добавляем контакт без ника (должен использоваться ID контакта)
	err = srv.db.CreateUser("user3@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	err = sendRequest(clientConn, "add|user3@example.com")
	if err != nil {
		t.Fatalf("Failed to send add: %v", err)
	}

	response, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected = "ok|add"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Пытаемся добавить несуществующего пользователя
	err = sendRequest(clientConn, "add|nonexistent@example.com|Friend")
	if err != nil {
		t.Fatalf("Failed to send add: %v", err)
	}

	response, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "fail|add|User not found") {
		t.Errorf("Expected fail|add|User not found, got %q", response)
	}
}

// TestRenameContact тестирует команду ren
func TestRenameContact(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Добавляем контакт
	err = srv.db.AddContact("user1@example.com", "user2@example.com", "Old Nick")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Переименовываем контакт
	err = sendRequest(clientConn, "ren|user2@example.com|New Nick")
	if err != nil {
		t.Fatalf("Failed to send ren: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|ren"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Проверяем, что ник изменился
	err = sendRequest(clientConn, "list")
	if err != nil {
		t.Fatalf("Failed to send list: %v", err)
	}

	listResponse, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read list response: %v", err)
	}

	if !strings.Contains(listResponse, "user2@example.com|New Nick") {
		t.Errorf("Expected user2@example.com|New Nick in list, got %q", listResponse)
	}
}

// TestDeleteContact тестирует команду del
func TestDeleteContact(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Добавляем контакт
	err = srv.db.AddContact("user1@example.com", "user2@example.com", "Friend")
	if err != nil {
		t.Fatalf("Failed to add contact: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Удаляем контакт
	err = sendRequest(clientConn, "del|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send del: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|del"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}

	// Проверяем, что контакт удален
	err = sendRequest(clientConn, "list")
	if err != nil {
		t.Fatalf("Failed to send list: %v", err)
	}

	listResponse, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read list response: %v", err)
	}

	if strings.Contains(listResponse, "user2@example.com") {
		t.Errorf("Contact should be deleted, but found in list: %q", listResponse)
	}
}

// TestBye тестирует команду bye
func TestBye(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователя
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Отправляем bye
	err = sendRequest(clientConn, "bye")
	if err != nil {
		t.Fatalf("Failed to send bye: %v", err)
	}

	// Проверяем ответ
	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "bye"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestHelp тестирует команду help
func TestHelp(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Отправляем help (не требует авторизации)
	err := sendRequest(clientConn, "help")
	if err != nil {
		t.Fatalf("Failed to send help: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "help|") {
		t.Errorf("Expected help|..., got %q", response)
	}

	// Проверяем, что в ответе есть основные команды
	expectedCommands := []string{"ping", "auth", "reg", "msg", "ack", "hist", "hclear", "stat", "list", "add", "ren", "del", "bye", "help"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(response, cmd) {
			t.Errorf("Expected command %q in help response, got %q", cmd, response)
		}
	}
}

// TestMessageToNonContact тестирует отправку сообщения контакту не из списка
func TestMessageToNonContact(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("sender@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("recipient@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Создаем соединения
	senderServerConn, senderClientConn := createTestConnection()
	recipientServerConn, recipientClientConn := createTestConnection()
	defer senderServerConn.Close()
	defer senderClientConn.Close()
	defer recipientServerConn.Close()
	defer recipientClientConn.Close()

	go func() {
		srv.handleConnection(senderServerConn)
	}()
	go func() {
		srv.handleConnection(recipientServerConn)
	}()

	// Авторизуем отправителя
	err = sendRequest(senderClientConn, "auth|sender@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Авторизуем получателя
	err = sendRequest(recipientClientConn, "auth|recipient@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Отправляем сообщение (получатель не в списке контактов)
	err = sendRequest(senderClientConn, "msg|recipient@example.com|Hello!")
	if err != nil {
		t.Fatalf("Failed to send msg: %v", err)
	}

	// Проверяем, что получатель получил сообщение (может прийти раньше ответа отправителю)
	msgResponse, err := readResponse(recipientClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	if !strings.HasPrefix(msgResponse, "msg|sender@example.com|Hello!|") {
		t.Errorf("Expected msg|sender@example.com|Hello!|..., got %q", msgResponse)
	}

	// Проверяем ответ отправителю
	response, err := readResponse(senderClientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|msg"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestStatusSpecificUser тестирует запрос статуса конкретного пользователя
func TestStatusSpecificUser(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Запрашиваем статус несуществующего пользователя
	err = sendRequest(clientConn, "stat|nonexistent@example.com")
	if err != nil {
		t.Fatalf("Failed to send stat: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "fail|stat|User not found") {
		t.Errorf("Expected fail|stat|User not found, got %q", response)
	}

	// Запрашиваем статус существующего пользователя (оффлайн)
	err = sendRequest(clientConn, "stat|user2@example.com")
	if err != nil {
		t.Fatalf("Failed to send stat: %v", err)
	}

	response, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "stat|user2@example.com|off"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestHistoryPagination тестирует пагинацию истории
func TestHistoryPagination(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Создаем много сообщений
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		sender := "user1@example.com"
		if i%2 == 0 {
			sender = "user2@example.com"
		}
		err = srv.db.SaveMessage(sender, "user1@example.com", "Message "+string(rune('0'+i)), timestamp)
		if err != nil {
			t.Fatalf("Failed to save message: %v", err)
		}
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Запрашиваем первые 5 сообщений
	err = sendRequest(clientConn, "hist|user2@example.com|5")
	if err != nil {
		t.Fatalf("Failed to send hist: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "hist|user2@example.com|") {
		t.Errorf("Expected hist|user2@example.com|..., got %q", response)
	}

	// Запрашиваем с offset
	err = sendRequest(clientConn, "hist|user2@example.com|5|5")
	if err != nil {
		t.Fatalf("Failed to send hist: %v", err)
	}

	response, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "hist|user2@example.com|") {
		t.Errorf("Expected hist|user2@example.com|..., got %q", response)
	}
}

// TestEscapeCharacters тестирует экранирование специальных символов
func TestEscapeCharacters(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// Создаем пользователей
	err := srv.db.CreateUser("user1@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	err = srv.db.CreateUser("user2@example.com", "password123")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Авторизуемся
	err = sendRequest(clientConn, "auth|user1@example.com|password123")
	if err != nil {
		t.Fatalf("Failed to send auth: %v", err)
	}
	_, err = readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read auth response: %v", err)
	}

	// Отправляем сообщение со специальными символами
	message := "Hello|World,Test\\Backslash\nNewline"
	err = sendRequest(clientConn, "msg|user2@example.com|"+protocol.Escape(message))
	if err != nil {
		t.Fatalf("Failed to send msg: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := "ok|msg"
	if response != expected {
		t.Errorf("Expected %q, got %q", expected, response)
	}
}

// TestUnauthenticatedAccess тестирует доступ без авторизации
func TestUnauthenticatedAccess(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	serverConn, clientConn := createTestConnection()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		srv.handleConnection(serverConn)
	}()

	// Пытаемся выполнить команду, требующую авторизации
	err := sendRequest(clientConn, "msg|user@example.com|Hello")
	if err != nil {
		t.Fatalf("Failed to send msg: %v", err)
	}

	response, err := readResponse(clientConn, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "fail|msg|Not authenticated") {
		t.Errorf("Expected fail|msg|Not authenticated, got %q", response)
	}
}
