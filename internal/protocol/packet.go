// Package protocol определяет бинарные структуры пакетов для сетевой коммуникации.
// Используется FlatBuffers для zero-copy сериализации.
package protocol

import (
	"encoding/binary"
	"io"
	"math"
)

// PacketType определяет тип пакета в протоколе
type PacketType uint8

const (
	// Служебные пакеты
	PacketClientHello   PacketType = 0x01
	PacketServerWelcome PacketType = 0x02
	PacketPing          PacketType = 0x03
	PacketPong          PacketType = 0x04
	PacketDisconnect    PacketType = 0xFF

	// Пакеты движения
	PacketMoveRequest PacketType = 0x10
	PacketMoveUpdate  PacketType = 0x11
	PacketMoveBatch   PacketType = 0x12 // Пакетное обновление нескольких игроков

	// Пакеты действий
	PacketActionRequest    PacketType = 0x20
	PacketActionResult     PacketType = 0x21
	PacketAttackRequest    PacketType = 0x22
	PacketAttackResult     PacketType = 0x23
	PacketBuildRequest     PacketType = 0x24
	PacketBuildResult      PacketType = 0x25
	PacketPickupRequest    PacketType = 0x26
	PacketPickupResult     PacketType = 0x27

	// Пакеты состояния
	PacketHealthUpdate   PacketType = 0x30
	PacketInventoryUpdate PacketType = 0x31
	PacketSpawnEvent     PacketType = 0x32
	PacketDeathEvent     PacketType = 0x33

	// Чат и социалка
	PacketChatMessage PacketType = 0x40
	PacketClanInvite  PacketType = 0x41
	PacketClanUpdate  PacketType = 0x42

	// Обновления мира
	PacketWorldState   PacketType = 0x50
	PacketObjectSpawn  PacketType = 0x51
	PacketObjectDespawn PacketType = 0x52
	PacketObjectUpdate PacketType = 0x53
)

// MoveFlags определяет флаги движения
type MoveFlags uint8

const (
	FlagWalking     MoveFlags = 1 << iota // Игрок идёт
	FlagRunning                           // Игрок бежит
	FlagCrouching                         // Игрок присел
	FlagJumping                           // Игрок прыгает
	FlagFlying                            // Чит-флаг (полёт)
	FlagInVehicle                         // В транспорте
)

// ============================================================================
// Структуры пакетов (размеры указаны в байтах)
// ============================================================================

// PacketHeader - заголовок каждого пакета (5 байт)
// [PacketType:1][Length:2][SequenceID:2]
type PacketHeader struct {
	Type       PacketType
	Length     uint16 // Длина данных после заголовка
	SequenceID uint16 // Порядковый номер для надёжности
}

const PacketHeaderSize = 5

// ClientHello - первый пакет от клиента при подключении (16+ байт)
// [ProtocolVersion:1][ClientID:8][Nickname:variable][SessionToken:variable]
type ClientHello struct {
	ProtocolVersion uint8
	ClientID        uint64 // Уникальный ID клиента
	Nickname        string
	SessionToken    string // Токен авторизации (если есть)
}

func (c *ClientHello) Encode() []byte {
	nickLen := len(c.Nickname)
	tokenLen := len(c.SessionToken)
	totalLen := 1 + 8 + 2 + nickLen + 2 + tokenLen

	buf := make([]byte, totalLen)
	offset := 0

	buf[offset] = c.ProtocolVersion
	offset++

	binary.LittleEndian.PutUint64(buf[offset:], c.ClientID)
	offset += 8

	binary.LittleEndian.PutUint16(buf[offset:], uint16(nickLen))
	offset += 2

	copy(buf[offset:], c.Nickname)
	offset += nickLen

	binary.LittleEndian.PutUint16(buf[offset:], uint16(tokenLen))
	offset += 2

	copy(buf[offset:], c.SessionToken)

	return buf
}

// ServerWelcome - ответ сервера на подключение (20 байт)
// [PlayerID:4][WorldSeed:8][SpawnX:float32][SpawnY:float32][TickCount:4]
type ServerWelcome struct {
	PlayerID   uint32 // Уникальный ID игрока в этой комнате
	WorldSeed  int64  // Сид генерации мира
	SpawnX     float32
	SpawnY     float32
	TickCount  uint32 // Текущий тик сервера
	ServerTime int64  // Unix timestamp
}

func (s *ServerWelcome) Encode() []byte {
	buf := make([]byte, 28)
	offset := 0

	binary.LittleEndian.PutUint32(buf[offset:], s.PlayerID)
	offset += 4

	binary.LittleEndian.PutUint64(buf[offset:], uint64(s.WorldSeed))
	offset += 8

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(s.SpawnX))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(s.SpawnY))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], s.TickCount)
	offset += 4

	binary.LittleEndian.PutUint64(buf[offset:], uint64(s.ServerTime))

	return buf
}

// MoveRequest - запрос движения от клиента (17 байт)
// [PlayerID:4][X:float32][Y:float32][Rotation:float32][Flags:1][TickID:4]
type MoveRequest struct {
	PlayerID  uint32
	X         float32
	Y         float32
	Rotation  float32 // Угол поворота в радианах
	Flags     MoveFlags
	TickID    uint32 // Тик, на котором произошло движение
	InputSeq  uint16 // Последовательность ввода для интерполяции
}

func (m *MoveRequest) Encode() []byte {
	buf := make([]byte, 21)
	offset := 0

	binary.LittleEndian.PutUint32(buf[offset:], m.PlayerID)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.X))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.Y))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.Rotation))
	offset += 4

	buf[offset] = byte(m.Flags)
	offset++

	binary.LittleEndian.PutUint32(buf[offset:], m.TickID)
	offset += 4

	binary.LittleEndian.PutUint16(buf[offset:], m.InputSeq)

	return buf
}

// MoveUpdate - обновление позиции одного игрока (21 байт)
// [PlayerID:4][X:float32][Y:float32][Rotation:float32][Flags:1][AnimationState:2]
type MoveUpdate struct {
	PlayerID       uint32
	X              float32
	Y              float32
	Rotation       float32
	Flags          MoveFlags
	AnimationState uint16 // Состояние анимации
	VelocityX      float32 // Для интерполяции
	VelocityY      float32
}

func (m *MoveUpdate) Encode() []byte {
	buf := make([]byte, 29)
	offset := 0

	binary.LittleEndian.PutUint32(buf[offset:], m.PlayerID)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.X))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.Y))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.Rotation))
	offset += 4

	buf[offset] = byte(m.Flags)
	offset++

	binary.LittleEndian.PutUint16(buf[offset:], m.AnimationState)
	offset += 2

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.VelocityX))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(m.VelocityY))

	return buf
}

// MoveBatch - пакетное обновление позиций (переменный размер)
// [Count:2][MoveUpdate...][]
type MoveBatch struct {
	Updates []MoveUpdate
	TickID  uint32
}

func (m *MoveBatch) Encode() []byte {
	// Заголовок батча: count(2) + tickID(4)
	headerSize := 6
	dataSize := len(m.Updates) * 29 // Размер одного MoveUpdate

	buf := make([]byte, headerSize+dataSize)
	offset := 0

	binary.LittleEndian.PutUint16(buf[offset:], uint16(len(m.Updates)))
	offset += 2

	binary.LittleEndian.PutUint32(buf[offset:], m.TickID)
	offset += 4

	for _, update := range m.Updates {
		updateBytes := update.Encode()
		copy(buf[offset:], updateBytes)
		offset += len(updateBytes)
	}

	return buf
}

// ActionRequest - запрос действия (атака, строительство, подбор) (13 байт)
// [ActionType:1][TargetID:4][TargetX:float32][TargetY:float32][ItemID:2]
type ActionRequest struct {
	ActionType uint8
	TargetID   uint32
	TargetX    float32
	TargetY    float32
	ItemID     uint16
}

func (a *ActionRequest) Encode() []byte {
	buf := make([]byte, 15)
	offset := 0

	buf[offset] = a.ActionType
	offset++

	binary.LittleEndian.PutUint32(buf[offset:], a.TargetID)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(a.TargetX))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(a.TargetY))
	offset += 4

	binary.LittleEndian.PutUint16(buf[offset:], a.ItemID)

	return buf
}

// HealthUpdate - обновление здоровья (8 байт)
// [EntityID:4][Health:float32][MaxHealth:float32]
type HealthUpdate struct {
	EntityID  uint32
	Health    float32
	MaxHealth float32
}

func (h *HealthUpdate) Encode() []byte {
	buf := make([]byte, 12)
	offset := 0

	binary.LittleEndian.PutUint32(buf[offset:], h.EntityID)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(h.Health))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(h.MaxHealth))

	return buf
}

// ChatMessage - сообщение чата (переменный размер)
// [ChannelID:1][SenderID:4][Message:variable]
type ChatMessage struct {
	ChannelID uint8
	SenderID  uint32
	Message   string
}

func (c *ChatMessage) Encode() []byte {
	msgLen := len(c.Message)
	buf := make([]byte, 6+msgLen)
	offset := 0

	buf[offset] = c.ChannelID
	offset++

	binary.LittleEndian.PutUint32(buf[offset:], c.SenderID)
	offset += 4

	binary.LittleEndian.PutUint16(buf[offset:], uint16(msgLen))
	offset += 2

	copy(buf[offset:], c.Message)

	return buf
}

// WorldState - полное состояние объекта в мире (переменный размер)
// [ObjectType:1][ObjectID:4][X:float32][Y:float32][Data:variable]
type WorldState struct {
	ObjectType uint8
	ObjectID   uint32
	X          float32
	Y          float32
	Data       []byte // Зависит от типа объекта
}

func (w *WorldState) Encode() []byte {
	dataLen := len(w.Data)
	buf := make([]byte, 14+dataLen)
	offset := 0

	buf[offset] = w.ObjectType
	offset++

	binary.LittleEndian.PutUint32(buf[offset:], w.ObjectID)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(w.X))
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(w.Y))
	offset += 4

	binary.LittleEndian.PutUint16(buf[offset:], uint16(dataLen))
	offset += 2

	copy(buf[offset:], w.Data)

	return buf
}

// ============================================================================
// Утилиты для работы с пакетами
// ============================================================================

// ReadPacketHeader читает заголовок пакета из потока
func ReadPacketHeader(r io.Reader) (*PacketHeader, error) {
	buf := make([]byte, PacketHeaderSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}

	return &PacketHeader{
		Type:       PacketType(buf[0]),
		Length:     binary.LittleEndian.Uint16(buf[1:3]),
		SequenceID: binary.LittleEndian.Uint16(buf[3:5]),
	}, nil
}

// WritePacketHeader записывает заголовок пакета в буфер
func WritePacketHeader(buf []byte, header *PacketHeader) {
	buf[0] = byte(header.Type)
	binary.LittleEndian.PutUint16(buf[1:3], header.Length)
	binary.LittleEndian.PutUint16(buf[3:5], header.SequenceID)
}

// CompressFloat сжимает float32 до uint16 для экономии трафика (потеря точности)
func CompressFloat(val float32, minVal, maxVal float32) uint16 {
	normalized := (val - minVal) / (maxVal - minVal)
	return uint16(normalized * 65535)
}

// DecompressFloat восстанавливает float32 из uint16
func DecompressFloat(compressed uint16, minVal, maxVal float32) float32 {
	normalized := float32(compressed) / 65535.0
	return normalized*(maxVal-minVal) + minVal
}

// DeltaEncode кодирует дельту между значениями (для оптимизации трафика)
func DeltaEncode(current, previous float32) int16 {
	delta := current - previous
	if delta > 32767 {
		return 32767
	}
	if delta < -32768 {
		return -32768
	}
	return int16(delta * 100) // Масштабируем для сохранения точности
}

// DeltaDecode декодирует дельту обратно в значение
func DeltaDecode(delta int16, previous float32) float32 {
	return previous + float32(delta)/100.0
}
