// Package network реализует высокопроизводительный WebSocket сервер для игры.
// Поддерживает до 5000 одновременных подключений на один сервер.
package network

import (
	"context"
	"encoding/binary"
	"log"
	"net/http"
	"sync"
	"time"

	"devast-io-server/internal/protocol"
	"github.com/gorilla/websocket"
)

const (
	// WriteWait - время ожидания записи
	WriteWait = 10 * time.Second

	// PongWait - время ожидания pong ответа
	PongWait = 60 * time.Second

	// PingPeriod - период отправки ping (должен быть меньше PongWait)
	PingPeriod = (PongWait * 9) / 10

	// MaxMessageSize - максимальный размер сообщения
	MaxMessageSize = 4096

	// SendBufferSize - размер буфера отправки
	SendBufferSize = 256
)

// Connection представляет подключение одного клиента
type Connection struct {
	ID         uint32
	Conn       *websocket.Conn
	Hub        *Hub
	Send       chan []byte
	PlayerID   uint32
	Nickname   string
	IsAuthed   bool
	LastActive time.Time
	mu         sync.RWMutex
}

// Hub управляет всеми подключениями
type Hub struct {
	Connections map[uint32]*Connection
	Register    chan *Connection
	Unregister  chan *Connection
	Broadcast   chan []byte
	mu          sync.RWMutex
	nextID      uint32

	// Callbacks
	OnConnect    func(*Connection)
	OnDisconnect func(*Connection)
	OnMessage    func(*Connection, []byte)
}

// NewHub создаёт новый хаб
func NewHub() *Hub {
	return &Hub{
		Connections: make(map[uint32]*Connection),
		Register:    make(chan *Connection),
		Unregister:  make(chan *Connection),
		Broadcast:   make(chan []byte, 256),
		nextID:      1,
	}
}

// Run запускает хаб
func (h *Hub) Run() {
	for {
		select {
		case conn := <-h.Register:
			h.mu.Lock()
			conn.ID = h.nextID
			h.nextID++
			h.Connections[conn.ID] = conn
			h.mu.Unlock()

			if h.OnConnect != nil {
				h.OnConnect(conn)
			}
			log.Printf("[HUB] Connected: %d, total: %d", conn.ID, len(h.Connections))

		case conn := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Connections[conn.ID]; ok {
				delete(h.Connections, conn.ID)
				close(conn.Send)
			}
			h.mu.Unlock()

			if h.OnDisconnect != nil {
				h.OnDisconnect(conn)
			}
			log.Printf("[HUB] Disconnected: %d, total: %d", conn.ID, len(h.Connections))

		case message := <-h.Broadcast:
			h.mu.RLock()
			for _, conn := range h.Connections {
				select {
				case conn.Send <- message:
				default:
					// Буфер переполнен, отключаем
					close(conn.Send)
					delete(h.Connections, conn.ID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// GetConnectionCount возвращает количество подключений
func (h *Hub) GetConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.Connections)
}

// BroadcastToArea отправляет сообщение только игрокам в определённой области
func (h *Hub) BroadcastToArea(message []byte, playerIDs []uint32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, id := range playerIDs {
		if conn, ok := h.Connections[id]; ok {
			select {
			case conn.Send <- message:
			default:
				// Буфер переполнен
			}
		}
	}
}

// WritePump обрабатывает исходящие сообщения
func (c *Connection) WritePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if !ok {
				// Хаб закрыл канал
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Добавляем все ожидающие сообщения в тот же пакет
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump обрабатывает входящие сообщения
func (c *Connection) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(MaxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(PongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[CONN] Error: %v", err)
			}
			break
		}

		c.LastActive = time.Now()

		if c.Hub.OnMessage != nil {
			c.Hub.OnMessage(c, message)
		}
	}
}

// SendPacket отправляет бинарный пакет клиенту
func (c *Connection) SendPacket(packetType protocol.PacketType, data []byte) {
	header := make([]byte, protocol.PacketHeaderSize)
	header[0] = byte(packetType)
	binary.LittleEndian.PutUint16(header[1:3], uint16(len(data)))
	binary.LittleEndian.PutUint16(header[3:5], 0) // SequenceID

	message := append(header, data...)

	select {
	case c.Send <- message:
	default:
		// Буфер переполнен
	}
}

// ServerConfig конфигурация сервера
type ServerConfig struct {
	Host            string
	Port            int
	MaxConnections  int
	TickRate        int
	ReadBufferSize  int
	WriteBufferSize int
}

// DefaultServerConfig возвращает конфигурацию по умолчанию
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Host:            "0.0.0.0",
		Port:            8080,
		MaxConnections:  5000,
		TickRate:        20,
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

// GameServer игровой сервер
type GameServer struct {
	Config     *ServerConfig
	Hub        *Hub
	Server     *http.Server
	upgrader   websocket.Upgrader
	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  bool

	// Игровая логика
	GameUpdateCallback func(deltaTime float32)
}

// NewGameServer создаёт новый игровой сервер
func NewGameServer(config *ServerConfig) *GameServer {
	ctx, cancel := context.WithCancel(context.Background())

	server := &GameServer{
		Config: config,
		Hub:    NewHub(),
		ctx:    ctx,
		cancel: cancel,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  config.ReadBufferSize,
			WriteBufferSize: config.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				// Разрешаем все origins (в продакшене настроить!)
				return true
			},
		},
	}

	// Настраиваем обработчики хаба
	server.Hub.OnConnect = server.onClientConnect
	server.Hub.OnDisconnect = server.onClientDisconnect
	server.Hub.OnMessage = server.onClientMessage

	return server
}

// Start запускает сервер
func (gs *GameServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gs.handleWebSocket)
	mux.HandleFunc("/health", gs.handleHealth)
	mux.HandleFunc("/stats", gs.handleStats)

	gs.Server = &http.Server{
		Addr:         ":" + string(rune(gs.Config.Port)),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Запускаем хаб
	go gs.Hub.Run()

	// Запускаем игровой цикл
	go gs.gameLoop()

	gs.isRunning = true
	log.Printf("[SERVER] Starting on %s:%d", gs.Config.Host, gs.Config.Port)

	return gs.Server.ListenAndServe()
}

// Stop останавливает сервер
func (gs *GameServer) Stop() error {
	gs.isRunning = false
	gs.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return gs.Server.Shutdown(ctx)
}

// handleWebSocket обрабатывает WebSocket подключения
func (gs *GameServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := gs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}

	// Проверяем лимит подключений
	if gs.Hub.GetConnectionCount() >= gs.Config.MaxConnections {
		conn.Close()
		log.Printf("[WS] Connection rejected - server full")
		return
	}

	client := &Connection{
		Conn:       conn,
		Hub:        gs.Hub,
		Send:       make(chan []byte, SendBufferSize),
		LastActive: time.Now(),
		IsAuthed:   false,
	}

	gs.Hub.Register <- client

	// Запускаем помпы
	go client.WritePump()
	go client.ReadPump()
}

// handleHealth обрабатывает health check
func (gs *GameServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleStats обрабатывает запрос статистики
func (gs *GameServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// В продакшене использовать json.Encoder
	w.Write([]byte("{\"status\":\"ok\"}"))
}

// onClientConnect вызывается при подключении клиента
func (gs *GameServer) onClientConnect(conn *Connection) {
	log.Printf("[GAME] Client connected: %d", conn.ID)
}

// onClientDisconnect вызывается при отключении клиента
func (gs *GameServer) onClientDisconnect(conn *Connection) {
	log.Printf("[GAME] Client disconnected: %d", conn.ID)
}

// onClientMessage обрабатывает входящее сообщение
func (gs *GameServer) onClientMessage(conn *Connection, data []byte) {
	if len(data) < protocol.PacketHeaderSize {
		return
	}

	// Читаем заголовок
	packetType := protocol.PacketType(data[0])
	length := binary.LittleEndian.Uint16(data[1:3])

	log.Printf("[MSG] Type: 0x%02X, Length: %d", packetType, length)

	// Обрабатываем в зависимости от типа
	switch packetType {
	case protocol.PacketClientHello:
		gs.handleClientHello(conn, data[protocol.PacketHeaderSize:])
	case protocol.PacketMoveRequest:
		gs.handleMoveRequest(conn, data[protocol.PacketHeaderSize:])
	case protocol.PacketPing:
		gs.handlePing(conn)
	}
}

// handleClientHello обрабатывает приветствие клиента
func (gs *GameServer) handleClientHello(conn *Connection, data []byte) {
	// Парсим ClientHello
	// В продакшене реализовать полный парсинг

	conn.IsAuthed = true
	conn.Nickname = "Player"

	// Отправляем ServerWelcome
	welcome := protocol.ServerWelcome{
		PlayerID:   conn.ID,
		WorldSeed:  12345,
		SpawnX:     0,
		SpawnY:     0,
		TickCount:  0,
		ServerTime: time.Now().Unix(),
	}

	conn.SendPacket(protocol.PacketServerWelcome, welcome.Encode())
	log.Printf("[AUTH] Client %d authenticated as %s", conn.ID, conn.Nickname)
}

// handleMoveRequest обрабатывает запрос движения
func (gs *GameServer) handleMoveRequest(conn *Connection, data []byte) {
	if !conn.IsAuthed {
		return
	}

	// Парсим MoveRequest
	// В продакшене реализовать полный парсинг и обновление позиции
}

// handlePing обрабатывает ping
func (gs *GameServer) handlePing(conn *Connection) {
	conn.SendPacket(protocol.PacketPong, []byte{})
}

// gameLoop основной игровой цикл
func (gs *GameServer) gameLoop() {
	tickDuration := time.Second / time.Duration(gs.Config.TickRate)
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	lastTime := time.Now()

	for range ticker.C {
		if !gs.isRunning {
			break
		}

		now := time.Now()
		deltaTime := float32(now.Sub(lastTime).Seconds())
		lastTime = now

		// Вызываем игровой апдейт
		if gs.GameUpdateCallback != nil {
			gs.GameUpdateCallback(deltaTime)
		}

		// Здесь будет обновление позиций и отправка батчей
	}
}

// BroadcastToAll отправляет сообщение всем подключенным
func (gs *GameServer) BroadcastToAll(packetType protocol.PacketType, data []byte) {
	header := make([]byte, protocol.PacketHeaderSize)
	header[0] = byte(packetType)
	binary.LittleEndian.PutUint16(header[1:3], uint16(len(data)))

	message := append(header, data...)
	gs.Hub.Broadcast <- message
}

// GetPlayerCount возвращает количество игроков
func (gs *GameServer) GetPlayerCount() int {
	return gs.Hub.GetConnectionCount()
}

// GetConnection получает подключение по ID
func (gs *GameServer) GetConnection(id uint32) *Connection {
	gs.Hub.mu.RLock()
	defer gs.Hub.mu.RUnlock()
	return gs.Hub.Connections[id]
}
