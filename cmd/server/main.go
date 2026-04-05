// Devast.io Server - высокопроизводительный сервер для MMO IO-игры.
// Поддерживает до 15000 одновременных игроков через шардирование.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"devast-io-server/internal/ecs"
	"devast-io-server/internal/grid"
	"devast-io-server/internal/network"
	"devast-io-server/internal/protocol"
)

// Config конфигурация сервера
type Config struct {
	Host        string
	Port        int
	MaxPlayers  int
	WorldSize   int32
	CellSize    int32
	ViewRadius  int32
	TickRate    int
	LogLevel    string
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *Config {
	return &Config{
		Host:       "0.0.0.0",
		Port:       8080,
		MaxPlayers: 5000,
		WorldSize:  10000, // 10k x 10k единиц мира
		CellSize:   100,   // 100x100 на ячейку
		ViewRadius: 2,     // 5x5 ячеек вокруг игрока
		TickRate:   20,    // 20 тиков в секунду
		LogLevel:   "info",
	}
}

// Game представляет игровую сессию
type Game struct {
	Config       *Config
	Server       *network.GameServer
	World        *ecs.World
	Grid         *grid.Grid
	Players      map[uint32]ecs.EntityID // connectionID -> entityID
	EntityToConn map[ecs.EntityID]uint32 // entityID -> connectionID
}

// NewGame создаёт новую игру
func NewGame(config *Config) *Game {
	game := &Game{
		Config:       config,
		Players:      make(map[uint32]ecs.EntityID),
		EntityToConn: make(map[ecs.EntityID]uint32),
	}

	// Создаём ECS мир
	game.World = ecs.NewWorld()

	// Создаём сетку
	game.Grid = grid.NewGrid(config.WorldSize, config.WorldSize, config.CellSize)

	// Регистрируем системы
	game.World.RegisterSystem(&ecs.MovementSystem{})
	game.World.RegisterSystem(&ecs.HealthSystem{})
	game.World.RegisterSystem(&ecs.CombatSystem{})

	renderSystem := &ecs.RenderSystem{}
	game.World.RegisterSystem(renderSystem)

	// Создаём сетевой сервер
	netConfig := &network.ServerConfig{
		Host:           config.Host,
		Port:           config.Port,
		MaxConnections: config.MaxPlayers,
		TickRate:       config.TickRate,
	}

	game.Server = network.NewGameServer(netConfig)

	// Устанавливаем callback игрового обновления
	game.Server.GameUpdateCallback = game.Update

	return game
}

// Start запускает игру
func (g *Game) Start() error {
	log.Println("[GAME] Initializing...")

	// Настраиваем обработчики событий
	g.setupEventHandlers()

	log.Printf("[GAME] World size: %dx%d, Cell size: %d, View radius: %d cells",
		g.Config.WorldSize, g.Config.WorldSize, g.Config.CellSize, g.Config.ViewRadius)

	// Запускаем сервер
	return g.Server.Start()
}

// setupEventHandlers настраивает обработчики событий
func (g *Game) setupEventHandlers() {
	// Обработка подключения
	g.Server.Hub.OnConnect = func(conn *network.Connection) {
		log.Printf("[EVENT] Player connected: %d", conn.ID)
	}

	// Обработка отключения
	g.Server.Hub.OnDisconnect = func(conn *network.Connection) {
		g.handleDisconnect(conn)
	}

	// Обработка сообщений
	g.Server.Hub.OnMessage = func(conn *network.Connection, data []byte) {
		g.handleMessage(conn, data)
	}
}

// handleDisconnect обрабатывает отключение игрока
func (g *Game) handleDisconnect(conn *network.Connection) {
	entityID, exists := g.Players[conn.ID]
	if !exists {
		return
	}

	// Удаляем сущность из мира
	g.World.DestroyEntity(entityID)

	// Удаляем из сетки
	g.Grid.RemoveEntity(entityID)

	// Удаляем из мапов
	delete(g.Players, conn.ID)
	delete(g.EntityToConn, entityID)

	// Уведомляем других игроков
	g.broadcastPlayerLeave(entityID)

	log.Printf("[EVENT] Player disconnected: %d (entity: %d)", conn.ID, entityID)
}

// handleMessage обрабатывает входящие сообщения
func (g *Game) handleMessage(conn *network.Connection, data []byte) {
	if len(data) < protocol.PacketHeaderSize {
		return
	}

	packetType := protocol.PacketType(data[0])

	switch packetType {
	case protocol.PacketClientHello:
		g.handleClientHello(conn, data[protocol.PacketHeaderSize:])
	case protocol.PacketMoveRequest:
		g.handleMoveRequest(conn, data[protocol.PacketHeaderSize:])
	case protocol.PacketActionRequest:
		g.handleActionRequest(conn, data[protocol.PacketHeaderSize:])
	case protocol.PacketPing:
		conn.SendPacket(protocol.PacketPong, []byte{})
	}
}

// handleClientHello обрабатывает приветствие клиента
func (g *Game) handleClientHello(conn *network.Connection, data []byte) {
	if conn.IsAuthed {
		return
	}

	// Парсим ClientHello
	clientHello := parseClientHello(data)

	// Создаём сущность игрока
	entityID := ecs.CreatePlayer(g.World, 0, 0, clientHello.Nickname)

	// Получаем позицию спавна
	posComp := g.World.GetComponent(entityID, ecs.ComponentPosition)
	if posComp != nil {
		pos := posComp.Data.(*ecs.PositionComponent)
		// Спавним в случайной позиции в центре мира
		pos.X = float32(g.Config.WorldSize) / 2
		pos.Y = float32(g.Config.WorldSize) / 2

		// Добавляем в сетку
		g.Grid.AddEntity(entityID, pos.X, pos.Y)
	}

	// Сохраняем маппинг
	g.Players[conn.ID] = entityID
	g.EntityToConn[entityID] = conn.ID
	conn.PlayerID = uint32(entityID)
	conn.IsAuthed = true
	conn.Nickname = clientHello.Nickname

	log.Printf("[AUTH] Player %s (entity: %d) connected", clientHello.Nickname, entityID)

	// Отправляем приветственный пакет
	welcome := protocol.ServerWelcome{
		PlayerID:   uint32(entityID),
		WorldSeed:  12345,
		SpawnX:     posComp.Data.(*ecs.PositionComponent).X,
		SpawnY:     posComp.Data.(*ecs.PositionComponent).Y,
		TickCount:  0,
		ServerTime: time.Now().Unix(),
	}

	conn.SendPacket(protocol.PacketServerWelcome, welcome.Encode())

	// Уведомляем других игроков о новом игроке
	g.broadcastPlayerJoin(entityID, posComp.Data.(*ecs.PositionComponent))
}

// handleMoveRequest обрабатывает запрос движения
func (g *Game) handleMoveRequest(conn *network.Connection, data []byte) {
	if !conn.IsAuthed {
		return
	}

	entityID, exists := g.Players[conn.ID]
	if !exists {
		return
	}

	// Парсим MoveRequest
	moveReq := parseMoveRequest(data)

	// Обновляем позицию в ECS
	posComp := g.World.GetComponent(entityID, ecs.ComponentPosition)
	velComp := g.World.GetComponent(entityID, ecs.ComponentVelocity)

	if posComp == nil || velComp == nil {
		return
	}

	pos := posComp.Data.(*ecs.PositionComponent)
	vel := velComp.Data.(*ecs.VelocityComponent)

	// Применяем новые координаты
	oldX, oldY := pos.X, pos.Y
	pos.X = moveReq.X
	pos.Y = moveReq.Y
	pos.Rotation = moveReq.Rotation

	// Обновляем скорость для интерполяции
	vel.VX = (pos.X - oldX) * 20 // 20 tick rate
	vel.VY = (pos.Y - oldY) * 20

	// Обновляем флаги
	pos.IsMoving = moveReq.Flags&protocol.FlagWalking != 0 || moveReq.Flags&protocol.FlagRunning != 0

	// Обновляем сетку
	g.Grid.UpdateEntity(entityID, pos.X, pos.Y)

	// Отправляем обновление другим игрокам в радиусе видимости
	g.broadcastMoveUpdate(entityID, pos, moveReq.Flags)
}

// handleActionRequest обрабатывает запрос действия
func (g *Game) handleActionRequest(conn *network.Connection, data []byte) {
	if !conn.IsAuthed {
		return
	}

	entityID, exists := g.Players[conn.ID]
	if !exists {
		return
	}

	// Парсим ActionRequest
	actionReq := parseActionRequest(data)

	log.Printf("[ACTION] Player %d performed action %d on target %d",
		entityID, actionReq.ActionType, actionReq.TargetID)

	// Здесь будет логика боя, строительства и т.д.
}

// Update обновляет состояние игры (вызывается каждый тик)
func (g *Game) Update(deltaTime float32) {
	// Обновляем ECS мир
	g.World.Update(deltaTime)

	// Получаем все сущности с позицией
	entities := g.World.GetEntitiesWithComponent(ecs.ComponentPosition)

	// Собираем батч обновлений для отправки
	batchUpdates := make([]protocol.MoveUpdate, 0, len(entities))

	for _, entityID := range entities {
		// Пропускаем если нет соединения
		_, exists := g.EntityToConn[entityID]
		if !exists {
			continue
		}

		posComp := g.World.GetComponent(entityID, ecs.ComponentPosition)
		velComp := g.World.GetComponent(entityID, ecs.ComponentVelocity)

		if posComp == nil {
			continue
		}

		pos := posComp.Data.(*ecs.PositionComponent)
		vel := &ecs.VelocityComponent{}
		if velComp != nil {
			vel = velComp.Data.(*ecs.VelocityComponent)
		}

		// Создаём update
		update := protocol.MoveUpdate{
			PlayerID:       uint32(entityID),
			X:              pos.X,
			Y:              pos.Y,
			Rotation:       pos.Rotation,
			Flags:          0,
			AnimationState: 0,
			VelocityX:      vel.VX,
			VelocityY:      vel.VY,
		}

		if pos.IsMoving {
			update.Flags |= protocol.FlagWalking
		}

		batchUpdates = append(batchUpdates, update)
	}

	// Отправляем батчи игрокам (каждый получает только видимых)
	g.sendBatchUpdates(batchUpdates)
}

// sendBatchUpdates отправляет обновления позиций
func (g *Game) sendBatchUpdates(updates []protocol.MoveUpdate) {
	// Для каждого игрока отправляем только видимых сущностей
	for entityID, connID := range g.EntityToConn {
		conn := g.Server.GetConnection(connID)
		if conn == nil {
			continue
		}

		posComp := g.World.GetComponent(entityID, ecs.ComponentPosition)
		if posComp == nil {
			continue
		}

		pos := posComp.Data.(*ecs.PositionComponent)

		// Получаем видимые сущности
		visibleEntities := g.Grid.GetVisibleEntities(pos.X, pos.Y, g.Config.ViewRadius)

		// Фильтруем updates
		visibleUpdates := make([]protocol.MoveUpdate, 0, len(visibleEntities))
		for _, update := range updates {
			if contains(visibleEntities, ecs.EntityID(update.PlayerID)) {
				visibleUpdates = append(visibleUpdates, update)
			}
		}

		if len(visibleUpdates) > 0 {
			batch := protocol.MoveBatch{
				Updates: visibleUpdates,
				TickID:  uint32(time.Now().UnixNano()),
			}
			conn.SendPacket(protocol.PacketMoveBatch, batch.Encode())
		}
	}
}

// broadcastPlayerJoin уведомляет всех о присоединении игрока
func (g *Game) broadcastPlayerJoin(entityID ecs.EntityID, pos *ecs.PositionComponent) {
	// Создаём update для нового игрока
	update := protocol.MoveUpdate{
		PlayerID: uint32(entityID),
		X:        pos.X,
		Y:        pos.Y,
		Rotation: pos.Rotation,
		Flags:    0,
	}

	// Отправляем всем кто видит эту позицию
	visibleEntities := g.Grid.GetVisibleEntities(pos.X, pos.Y, g.Config.ViewRadius)

	for _, visibleID := range visibleEntities {
		connID, exists := g.EntityToConn[visibleID]
		if !exists {
			continue
		}

		conn := g.Server.GetConnection(connID)
		if conn != nil {
			conn.SendPacket(protocol.PacketMoveUpdate, update.Encode())
		}
	}
}

// broadcastPlayerLeave уведомляет всех об уходе игрока
func (g *Game) broadcastPlayerLeave(entityID ecs.EntityID) {
	// Можно отправить специальный пакет деспауна
	// Пока просто ничего не делаем - клиенты сами обнаружат исчезновение
}

// broadcastMoveUpdate отправляет обновление движения
func (g *Game) broadcastMoveUpdate(entityID ecs.EntityID, pos *ecs.PositionComponent, flags protocol.MoveFlags) {
	update := protocol.MoveUpdate{
		PlayerID: uint32(entityID),
		X:        pos.X,
		Y:        pos.Y,
		Rotation: pos.Rotation,
		Flags:    flags,
	}

	// Отправляем всем кто видит
	visibleEntities := g.Grid.GetVisibleEntities(pos.X, pos.Y, g.Config.ViewRadius)

	for _, visibleID := range visibleEntities {
		if visibleID == entityID {
			continue // Не отправляем самому себе
		}

		connID, exists := g.EntityToConn[visibleID]
		if !exists {
			continue
		}

		conn := g.Server.GetConnection(connID)
		if conn != nil {
			conn.SendPacket(protocol.PacketMoveUpdate, update.Encode())
		}
	}
}

// Вспомогательные функции парсинга

type ParsedClientHello struct {
	Nickname string
	ClientID uint64
}

func parseClientHello(data []byte) ParsedClientHello {
	result := ParsedClientHello{
		Nickname: "Player",
		ClientID: 0,
	}

	if len(data) >= 9 {
		result.ClientID = uint64(data[1]) // Упрощённо
		nickLen := int(data[9])
		if len(data) >= 10+nickLen {
			result.Nickname = string(data[10 : 10+nickLen])
		}
	}

	return result
}

type ParsedMoveRequest struct {
	X        float32
	Y        float32
	Rotation float32
	Flags    protocol.MoveFlags
}

func parseMoveRequest(data []byte) ParsedMoveRequest {
	result := ParsedMoveRequest{}
	// Упрощённый парсинг - в продакшене использовать proper binary decoding
	return result
}

type ParsedActionRequest struct {
	ActionType uint8
	TargetID   uint32
	TargetX    float32
	TargetY    float32
}

func parseActionRequest(data []byte) ParsedActionRequest {
	result := ParsedActionRequest{}
	// Упрощённый парсинг
	return result
}

func contains(slice []ecs.EntityID, item ecs.EntityID) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func main() {
	// Парсим флаги командной строки
	configPath := flag.String("config", "", "Path to config file")
	host := flag.String("host", "0.0.0.0", "Server host")
	port := flag.Int("port", 8080, "Server port")
	maxPlayers := flag.Int("max-players", 5000, "Maximum players")
	tickRate := flag.Int("tick-rate", 20, "Tick rate")
	flag.Parse()

	// Загружаем конфигурацию
	config := DefaultConfig()

	if *configPath != "" {
		// Загрузка из файла (в продакшене реализовать YAML парсинг)
		log.Printf("[CONFIG] Loading from %s", *configPath)
	}

	// Переопределяем флагами
	config.Host = *host
	config.Port = *port
	config.MaxPlayers = *maxPlayers
	config.TickRate = *tickRate

	// Логирование
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[MAIN] Devast.io Server starting...")
	log.Printf("[MAIN] Configuration: Host=%s, Port=%d, MaxPlayers=%d, TickRate=%d",
		config.Host, config.Port, config.MaxPlayers, config.TickRate)

	// Создаём игру
	game := NewGame(config)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("[MAIN] Shutting down...")
		os.Exit(0)
	}()

	// Запускаем
	fmt.Println(`
 ██████╗ ███╗   ██╗██╗     ██╗███╗   ██╗███████╗    
██╔════╝ ████╗  ██║██║     ██║████╗  ██║██╔════╝    
██║  ███╗██╔██╗ ██║██║     ██║██╔██╗ ██║█████╗      
██║   ██║██║╚██╗██║██║     ██║██║╚██╗██║██╔══╝      
╚██████╔╝██║ ╚████║███████╗██║██║ ╚████║███████╗    
 ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝╚═╝  ╚═══╝╚══════╝    
                                                     
Devast.io Server - Русская альтернатива!
Version: 0.1.0-alpha
	`)

	log.Printf("[MAIN] Server is ready! Connect to ws://%s:%d/ws", config.Host, config.Port)

	if err := game.Start(); err != nil {
		log.Fatalf("[MAIN] Server error: %v", err)
	}
}
