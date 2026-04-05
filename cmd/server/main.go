package main

import (
	"fmt"
	"log"
	"net/http" // Добавили этот импорт
	"os"
	"os/signal"
	"syscall"

	"devast-io-server/internal/network"
)

// Настройки сервера
type Config struct {
	Host       string
	Port       int
	MaxPlayers int
	TickRate   int
}

func DefaultConfig() Config {
	return Config{
		Host:       "0.0.0.0",
		Port:       8080,
		MaxPlayers: 5000,
		TickRate:   20,
	}
}

// Заглушка для игры
type Game struct {
	Config Config
	Server *network.GameServer
}

func NewGame(cfg Config) *Game {
	return &Game{Config: cfg}
}

func (g *Game) Start() error {
	log.Println("[GAME] Initializing...")
	log.Printf("[GAME] World size: 10000x10000, TickRate: %d", g.Config.TickRate)

	serverConfig := network.DefaultServerConfig()
	serverConfig.Host = g.Config.Host
	serverConfig.Port = g.Config.Port
	serverConfig.MaxConnections = g.Config.MaxPlayers
	serverConfig.TickRate = g.Config.TickRate

	g.Server = network.NewGameServer(serverConfig)

	return g.Server.Start()
}

func main() {
	config := DefaultConfig()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[MAIN] Devast.io Server starting...")

	game := NewGame(config)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("[MAIN] Shutting down...")
		if game.Server != nil {
			game.Server.Stop()
		}
		os.Exit(0)
	}()

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

	log.Printf("[MAIN] Server is ready!")
	log.Printf("[MAIN] Web Client: http://localhost:%d/", config.Port)
	log.Printf("[MAIN] WebSocket: ws://%s:%d/ws", config.Host, config.Port)

	// ЗАПУСК ВЕБ-СЕРВЕРА ДЛЯ КЛИЕНТА
	go func() {
		// Путь к папке public относительно cmd/server
		fs := http.FileServer(http.Dir("../../public"))
		http.Handle("/", fs)
		// Слушаем на том же порту, что и игра, но через стандартный http
		log.Println("[MAIN] Static files server started")
	}()

	if err := game.Start(); err != nil {
		log.Fatalf("[MAIN] Server error: %v", err)
	}
}
