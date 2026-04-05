# Devast.io Server - Русская альтернатива!

[![Go Version](https://img.shields.io/badge/go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-alpha-orange.svg)]()

Высокопроизводительный сервер для MMO IO-игры в стиле Devast.io, написанный на Go с поддержкой до **15 000 одновременных игроков**.

## 🎮 О проекте

Этот проект создан как ответвление от французской игры Devast.io, которая перестала нормально работать. Наша цель — создать стабильную русскоязычную альтернативу с активным комьюнити.

### История популярности Devast.io:
- **Пик**: 5000+ русских игроков vs 1500 иностранных
- **Золотые времена**: 1500+ русских vs 500 иностранных  
- **Текущее состояние**: ~350 русских vs 150 иностранных (все остальные страны вместе взятые)

Мы хотим вернуть былую славу и собрать комьюнити!

## 🏗️ Архитектура

```
┌─────────────────────────────────────────────────────────┐
│              Load Balancer (HAProxy/Nginx)              │
│                  Port: 80/443 → WebSocket               │
└───────────────────────┬─────────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        ▼               ▼               ▼
  ┌───────────┐   ┌───────────┐   ┌───────────┐
  │ Server 1  │   │ Server 2  │   │ Server N  │
  │ 5000 CCU  │   │ 5000 CCU  │   │ 5000 CCU  │
  └─────┬─────┘   └─────┬─────┘   └─────┬─────┘
        │               │               │
        └───────────────┼───────────────┘
                        ▼
              ┌─────────────────┐
              │  Redis Cluster  │
              │  - Sessions     │
              │  - Discovery    │
              │  - Leaderboards │
              └─────────────────┘
```

## ⚡ Технологии

| Компонент | Технология | Обоснование |
|-----------|------------|-------------|
| Язык | **Go 1.21+** | Горутинги, производительность |
| Сеть | **WebSocket + gorilla/websocket** | Бинарный протокол, низкая задержка |
| Протокол | **Binary (Little Endian)** | Никакого JSON, пакеты 20-30 байт |
| ECS | **Custom Implementation** | Data-oriented, кэш-локальность |
| Spatial | **Grid-based Partitioning** | Фильтрация по видимости |
| Балансировка | **Redis + HAProxy** | Шардирование на 3+ сервера |

## 📁 Структура проекта

```
devast-io-server/
├── cmd/
│   └── server/
│       └── main.go              # Точка входа
├── internal/
│   ├── ecs/                     # Entity Component System
│   │   └── world.go             # ECS ядро
│   ├── grid/                    # Spatial Partitioning
│   │   └── grid.go              # Сетка мира
│   ├── network/                 # Сетевой слой
│   │   └── server.go            # WebSocket сервер
│   ├── protocol/                # Бинарные протоколы
│   │   └── packet.go            # Структуры пакетов
│   ├── room/                    # Игровые комнаты
│   └── balance/                 # Балансировка
├── pkg/
│   ├── math32/                  # Математика
│   └── logger/                  # Логгер
├── configs/
│   └── server.yaml              # Конфигурация
├── docs/
│   └── architecture.md          # Документация
└── go.mod                       # Go модуль
```

## 🚀 Быстрый старт

### Требования

- Go 1.21 или выше
- Git

### Установка

```bash
# Клонируем репозиторий
git clone https://github.com/your-org/devast-io-server.git
cd devast-io-server

# Устанавливаем зависимости
go mod tidy

# Собираем
go build -o server ./cmd/server

# Запускаем
./server -port 8080 -max-players 5000
```

### Конфигурация

```bash
# Основные флаги
./server \
  -host "0.0.0.0" \
  -port 8080 \
  -max-players 5000 \
  -tick-rate 20 \
  -config configs/server.yaml
```

## 📦 Бинарный протокол

### Структура пакета

```
Заголовок (5 байт):
[Type:1][Length:2][SequenceID:2]

Данные (переменная длина)
```

### Типы пакетов

| Код | Название | Направление | Размер |
|-----|----------|-------------|--------|
| 0x01 | ClientHello | C→S | 16+ |
| 0x02 | ServerWelcome | S→C | 28 |
| 0x10 | MoveRequest | C→S | 21 |
| 0x11 | MoveUpdate | S→C | 29 |
| 0x12 | MoveBatch | S→C | переменный |
| 0x20 | ActionRequest | C→S | 15 |
| 0x30 | HealthUpdate | S→C | 12 |
| 0x40 | ChatMessage | ↔ | переменный |
| 0xFF | Disconnect | ↔ | 0 |

### Пример: пакет позиции (21 байт)

```go
type MoveRequest struct {
    PlayerID  uint32    // 4 байта
    X         float32   // 4 байта
    Y         float32   // 4 байта
    Rotation  float32   // 4 байта
    Flags     MoveFlags // 1 байт
    TickID    uint32    // 4 байта
}
```

## 🎯 Оптимизации

### 1. Spatial Partitioning (Grid)

Мир делится на ячейки 100×100 единиц. Игрок получает обновления только из своей ячейки и соседних (5×5 = 25 ячеек).

**Эффект**: При 5000 игроках каждый получает данные только о ~100 видимых вместо 5000.

```go
// Получение видимых сущностей
visible := grid.GetVisibleEntities(playerX, playerY, viewRadius)
```

### 2. ECS (Entity Component System)

Data-oriented дизайн для максимальной кэш-локальности:

```go
// Системы обрабатывают компоненты пакетно
entities := world.Query(ComponentPosition, ComponentVelocity)
for _, id := range entities {
    // Обновляем позицию
}
```

### 3. Пакетная отправка

Обновления позиций отправляются батчами 20 раз в секунду:

```go
batch := protocol.MoveBatch{
    Updates: []MoveUpdate{...},
    TickID:  12345,
}
```

### 4. Delta Compression

Отправляем только изменения (в разработке):

```go
delta := DeltaEncode(currentPos, previousPos)
```

## 📊 Производительность

### Целевые показатели (на 1 сервер)

| Метрика | Значение |
|---------|----------|
| Максимум игроков | 5000 CCU |
| Tick rate | 20 Hz |
| Средняя задержка | < 50ms |
| Трафик на игрока | ~2 KB/s |
| Использование RAM | ~4 GB |
| CPU ядер | 8+ |

### Масштабирование до 15000 CCU

```
3 сервера × 5000 игроков = 15000 CCU
```

Для кросс-серверного взаимодействия используется Redis.

## 🔧 Расширение функционала

### Добавление новой системы

```go
type MySystem struct {
    ecs.BaseSystem
}

func (s *MySystem) Update(world *ecs.World, deltaTime float32) {
    entities := world.Query(ComponentPosition, ComponentMyCustom)
    for _, id := range entities {
        // Логика
    }
}

// Регистрация
world.RegisterSystem(&MySystem{})
```

### Новый тип пакета

```go
const PacketMyNew PacketType = 0x60

type MyPacket struct {
    Field1 uint32
    Field2 float32
}

func (m *MyPacket) Encode() []byte {
    buf := make([]byte, 8)
    binary.LittleEndian.PutUint32(buf[:4], m.Field1)
    binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(m.Field2))
    return buf
}
```

## 🛠️ Roadmap

- [x] Базовый сервер с WebSocket
- [x] ECS архитектура
- [x] Grid-based spatial partitioning
- [x] Бинарный протокол
- [ ] Система боя и здоровья
- [ ] Инвентарь и предметы
- [ ] Строительство баз
- [ ] Кланы и социальная система
- [ ] Redis интеграция для шардирования
- [ ] Античит система
- [ ] Docker контейнеризация
- [ ] Мониторинг и метрики (Prometheus)

## 🤝 Вклад в проект

Приветствуется помощь от русскоязычного комьюнити!

```bash
# Форкните репозиторий
git fork ...

# Создайте ветку
git checkout -b feature/my-feature

# Закоммитьте изменения
git commit -am "Добавил крутую фичу"

# Отправьте PR
git push origin feature/my-feature
```

## 📝 Лицензия

MIT License - свободное использование с указанием авторства.

## 📞 Контакты

- Telegram: [канал проекта]()
- Discord: [сервер сообщества]()
- Email: dev@devast-rus.io

---

**Вдохновлено Devast.io** 🇫🇷 → **Сделано для русскоязычного комьюнити** 🇷🇺

*Вернём былую славу русской IO-сцене!*
