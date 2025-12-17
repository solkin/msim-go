package models

import "time"

type User struct {
	ID       int64
	Login    string
	Password string // hashed
}

type Contact struct {
	ID      int64
	Owner   string
	Contact string
	Nick    string
}

type Message struct {
	ID        int64
	Sender    string
	Recipient string
	Text      string
	Timestamp time.Time
	Status    string // "sent" or "ackn"
}

type Session struct {
	Login     string
	Conn      interface{} // будет *net.Conn, но здесь interface{} для избежания циклических зависимостей
	LastPing  time.Time
}

