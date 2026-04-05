# Devast.io Server Architecture (Go)

## Общая архитектура

```
┌─────────────────────────────────────────────────────────────────┐
│                    Load Balancer (HAProxy/Nginx)                │
│                    Port: 80/443 → WebSocket Upgrade             │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
    ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
    │  Game Server 1  │ │  Game Server 2  │ │  Game Server N  │
    │   (Go + uWS)    │ │   (Go + uWS)    │ │   (Go + uWS)    │
    │   5000 CCU      │ │   5000 CCU      │ │   5000 CCU      │
    └────────┬────────┘ └────────┬────────┘ └────────┬────────┘
             │                   │                   │
             └───────────────────┼───────────────────┘
                                 ▼
                    ┌─────────────────────────┐
                    │      Redis Cluster      │
                    │  - Room Discovery       │
                    │  - Player Session Store │
                    │  - Cross-server Chat    │
                    │  - Leaderboards         │
                    └─────────────────────────┘
```

## Структура проекта

```
devast-io-server/
├── cmd/
│   └── server/
│       └── main.go              # Точка входа сервера
├── internal/
│   ├── ecs/                     # Entity Component System
│   │   ├── entity.go            # ID сущностей
│   │   ├── component.go         # Компоненты (Position, Health, Inventory)
│   │   ├── system.go            # Системы (Movement, Combat, Render)
│   │   └── world.go             # Управление миром
│   ├── grid/                    # Spatial Partitioning
│   │   ├── grid.go              # Основная сетка
│   │   ├── cell.go              # Ячейка сетки
│   │   └── query.go             # Запросы по радиусу
│   ├── network/                 # Сетевой слой
│   │   ├── server.go            # WebSocket сервер
│   │   ├── connection.go        # Управление соединением
│   │   └── handler.go           # Обработка сообщений
│   ├── protocol/                # Бинарные протоколы
│   │   ├── packet.go            # Типы пакетов
│   │   ├── encoder.go           # Кодирование/декодирование
│   │   └── schema.fbs           # FlatBuffers схема
│   ├── room/                    # Игровые комнаты
│   │   ├── room.go              # Логика комнаты
│   │   ├── manager.go           # Менеджер комнат
│   │   └── config.go            # Конфигурация комнаты
│   └── balance/                 # Балансировка
│       ├── loadbalancer.go      # Логика балансировки
│       └── redis.go             # Redis интеграция
├── pkg/
│   ├── math32/                  # Математические утилиты
│   │   └── vector.go            # Векторные операции
│   └── logger/                  # Логирование
│       └── logger.go            # Настройка логгера
├── configs/
│   ├── server.yaml              # Конфигурация сервера
│   └── rooms.yaml               # Конфигурация комнат
├── scripts/
│   ├── build.sh                 # Скрипт сборки
│   └── deploy.sh                # Скрипт деплоя
├── docs/
│   ├── architecture.md          # Документация архитектуры
│   └── protocol.md              # Описание протокола
├── go.mod                       # Go модуль
├── go.sum                       # Зависимости
└── README.md                    # Этот файл
```

## Производительность и оптимизации

### 1. Spatial Partitioning (Grid-based)
- Мир делится на ячейки 100x100 единиц
- Игрок получает обновления только из своей ячейки и 8 соседних
- Снижение трафика в 100+ раз для 15000 игроков

### 2. ECS (Entity Component System)
- Data-oriented дизайн для кэш-локальности
- Параллельная обработка систем через goroutines
- Zero-allocation в горячих путях

### 3. Бинарный протокол (FlatBuffers)
- Никакого JSON - только бинарные данные
- Zero-copy десериализация
- Размер пакета позиции: ~20 байт

### 4. Оптимизации сети
- Пакетное обновление позиций (tick rate: 20Hz)
- Delta compression (отправка только изменений)
- Interpolation на клиенте для плавности

## Масштабирование до 15000 CCU

### Стратегия шардирования:
- 3 игровых сервера × 5000 игроков = 15000 CCU
- Каждый сервер имеет несколько комнат (по 100-500 игроков)
- Redis для кросс-серверной коммуникации

### Требования к железу (на 1 сервер):
- CPU: 8+ ядер (горизонтальное масштабирование горутин)
- RAM: 16-32 GB (кэш мира + состояния игроков)
- Network: 1 Gbps+ (бинарный протокол снижает нагрузку)

## Протокол общения

### Типы пакетов:
```
0x01 - ClientHello (клиент → сервер)
0x02 - ServerWelcome (сервер → клиент)
0x10 - MoveRequest (клиент → сервер)
0x11 - MoveUpdate (сервер → клиент, пакетный)
0x20 - ActionRequest (клиент → сервер)
0x21 - ActionResult (сервер → клиент)
0x30 - ChatMessage (двусторонний)
0xFF - Disconnect (двусторонний)
```

### Структура пакета позиции (20 байт):
```
[PacketType:1][PlayerID:4][X:float32:4][Y:float32:4][Rotation:float32:4][Flags:3]
```

## Запуск проекта

```bash
# Установка зависимостей
go mod tidy

# Сборка
go build -o server ./cmd/server

# Запуск
./server -config configs/server.yaml

# Или с Docker
docker-compose up -d
```

## Конфигурация (configs/server.yaml)

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  max_connections: 5000
  tick_rate: 20
  
grid:
  cell_size: 100
  view_radius: 3  # 3x3 ячейки вокруг игрока
  
room:
  max_players: 500
  min_players: 10
  auto_create: true
  
redis:
  host: "localhost"
  port: 6379
  db: 0
  
logging:
  level: "info"
  format: "json"
```

## План развития

1. **Фаза 1**: Базовый сервер с движением игроков
2. **Фаза 2**: Система боя и здоровья
3. **Фаза 3**: Инвентарь и крафт
4. **Фаза 4**: Строительство баз
5. **Фаза 5**: Кланы и социальная система
6. **Фаза 6**: Оптимизация и масштабирование

---

*Вдохновлено Devast.io - создаём лучшую русскоязычную IO-игру!*
