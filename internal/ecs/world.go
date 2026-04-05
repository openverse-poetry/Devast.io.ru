// Package ecs реализует Entity Component System (ECS) для управления игровыми сущностями.
// ECS подход обеспечивает высокую производительность за счёт data-oriented дизайна.
package ecs

import (
	"sync"
)

// EntityID - уникальный идентификатор сущности
type EntityID uint32

// ComponentType определяет тип компонента
type ComponentType uint8

const (
	ComponentPosition ComponentType = iota
	ComponentVelocity
	ComponentHealth
	ComponentRender
	ComponentInventory
	ComponentCombat
	ComponentBuild
	ComponentVehicle
	ComponentMax
)

// PositionComponent - компонент позиции
type PositionComponent struct {
	X, Y       float32
	Rotation   float32 // Угол в радианах
	Layer      uint8   // Слой для рендеринга (земля, объекты, воздух)
	IsMoving   bool
}

// VelocityComponent - компонент скорости
type VelocityComponent struct {
	VX, VY     float32
	Speed      float32
	MaxSpeed   float32
	Acceleration float32
	Friction   float32
}

// HealthComponent - компонент здоровья
type HealthComponent struct {
	Current    float32
	Max        float32
	Regen      float32 // Регенерация в секунду
	Armor      float32
	Flags      uint8   // Флаги состояния (отравлен, горит и т.д.)
	IsDead     bool
}

// RenderComponent - компонент рендеринга
type RenderComponent struct {
	SpriteID   uint16
	Color      uint32 // RGBA packed
	Scale      float32
	Rotation   float32
	Animation  uint16
	Frame      uint8
	Visible    bool
	Priority   uint8   // Приоритет рендеринга
}

// InventoryComponent - компонент инвентаря
type InventoryComponent struct {
	Slots      []uint32 // IDs предметов
	SelectedSlot uint8
	Capacity   uint8
	Weight     float32
	MaxWeight  float32
}

// CombatComponent - компонент боя
type CombatComponent struct {
	Damage     float32
	Range      float32
	FireRate   float32 // Выстрелов в секунду
	LastAttack int64   // Timestamp последнего выстрела
	Ammo       uint16
	MaxAmmo    uint16
	ReloadTime int64
	IsReloading bool
}

// BuildComponent - компонент строительства
type BuildComponent struct {
	BlueprintID uint16
	BuildProgress float32
	BuildHealth   float32
	MaxBuildHealth float32
	Materials     map[uint32]int // Required materials
	IsBuilding    bool
}

// VehicleComponent - компонент транспорта
type VehicleComponent struct {
	VehicleID   uint16
	Fuel        float32
	MaxFuel     float32
	Passengers  []EntityID
	DriverID    EntityID
	Speed       float32
	TurnRate    float32
	IsEngineOn  bool
}

// Component хранит данные компонента любого типа
type Component struct {
	Type ComponentType
	Data interface{}
}

// Entity представляет игровую сущность
type Entity struct {
	ID         EntityID
	Components [ComponentMax]*Component
	IsActive   bool
	Version    uint32 // Для проверки актуальности
}

// World - основной контейнер ECS
type World struct {
	Entities     map[EntityID]*Entity
	NextEntityID EntityID
	EntityCount  int
	mu           sync.RWMutex

	// Индексы для быстрого поиска
	entitiesByComponent [ComponentMax]map[EntityID]bool

	// Системы
	Systems []System

	// Callbacks
	onEntityCreated func(EntityID)
	onEntityDestroyed func(EntityID)
}

// NewWorld создаёт новый мир
func NewWorld() *World {
	world := &World{
		Entities:     make(map[EntityID]*Entity, 10000),
		NextEntityID: 1,
	}

	// Инициализируем индексы
	for i := range world.entitiesByComponent {
		world.entitiesByComponent[i] = make(map[EntityID]bool)
	}

	return world
}

// CreateEntity создаёт новую сущность
func (w *World) CreateEntity() EntityID {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.NextEntityID
	w.NextEntityID++

	entity := &Entity{
		ID:       id,
		IsActive: true,
		Version:  1,
	}

	w.Entities[id] = entity
	w.EntityCount++

	if w.onEntityCreated != nil {
		w.onEntityCreated(id)
	}

	return id
}

// DestroyEntity удаляет сущность
func (w *World) DestroyEntity(id EntityID) {
	w.mu.Lock()
	defer w.mu.Unlock()

	entity, exists := w.Entities[id]
	if !exists || !entity.IsActive {
		return
	}

	// Удаляем все компоненты
	for i := ComponentType(0); i < ComponentMax; i++ {
		if entity.Components[i] != nil {
			delete(w.entitiesByComponent[i], id)
			entity.Components[i] = nil
		}
	}

	entity.IsActive = false
	entity.Version++
	w.EntityCount--

	if w.onEntityDestroyed != nil {
		w.onEntityDestroyed(id)
	}
}

// AddComponent добавляет компонент к сущности
func (w *World) AddComponent(id EntityID, compType ComponentType, data interface{}) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	entity, exists := w.Entities[id]
	if !exists || !entity.IsActive {
		return false
	}

	if entity.Components[compType] != nil {
		return false // Уже есть такой компонент
	}

	entity.Components[compType] = &Component{
		Type: compType,
		Data: data,
	}

	w.entitiesByComponent[compType][id] = true
	return true
}

// RemoveComponent удаляет компонент у сущности
func (w *World) RemoveComponent(id EntityID, compType ComponentType) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	entity, exists := w.Entities[id]
	if !exists || !entity.IsActive {
		return false
	}

	if entity.Components[compType] == nil {
		return false
	}

	delete(w.entitiesByComponent[compType], id)
	entity.Components[compType] = nil
	return true
}

// GetComponent получает компонент сущности
func (w *World) GetComponent(id EntityID, compType ComponentType) *Component {
	w.mu.RLock()
	defer w.mu.RUnlock()

	entity, exists := w.Entities[id]
	if !exists || !entity.IsActive {
		return nil
	}

	return entity.Components[compType]
}

// HasComponent проверяет наличие компонента
func (w *World) HasComponent(id EntityID, compType ComponentType) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	entity, exists := w.Entities[id]
	if !exists || !entity.IsActive {
		return false
	}

	return entity.Components[compType] != nil
}

// GetEntitiesWithComponent возвращает все сущности с указанным компонентом
func (w *World) GetEntitiesWithComponent(compType ComponentType) []EntityID {
	w.mu.RLock()
	defer w.mu.RUnlock()

	index := w.entitiesByComponent[compType]
	result := make([]EntityID, 0, len(index))

	for id := range index {
		result = append(result, id)
	}

	return result
}

// Query_entities с несколькими компонентами
func (w *World) Query(requiredComponents ...ComponentType) []EntityID {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var result []EntityID

	// Берём наименьший индекс для оптимизации
	if len(requiredComponents) == 0 {
		return result
	}

	smallestIndex := w.entitiesByComponent[requiredComponents[0]]
	for id := range smallestIndex {
		hasAll := true
		for _, compType := range requiredComponents[1:] {
			if !w.entitiesByComponent[compType][id] {
				hasAll = false
				break
			}
		}
		if hasAll {
			result = append(result, id)
		}
	}

	return result
}

// System - интерфейс для систем обработки
type System interface {
	Update(world *World, deltaTime float32)
	Init(world *World)
}

// BaseSystem - базовая реализация системы
type BaseSystem struct{}

func (b *BaseSystem) Update(world *World, deltaTime float32) {}
func (b *BaseSystem) Init(world *World) {}

// MovementSystem обрабатывает движение сущностей
type MovementSystem struct {
	BaseSystem
}

func (m *MovementSystem) Update(world *World, deltaTime float32) {
	entities := world.Query(ComponentPosition, ComponentVelocity)

	for _, id := range entities {
		posComp := world.GetComponent(id, ComponentPosition)
		velComp := world.GetComponent(id, ComponentVelocity)

		if posComp == nil || velComp == nil {
			continue
		}

		pos := posComp.Data.(*PositionComponent)
		vel := velComp.Data.(*VelocityComponent)

		// Применяем скорость
		pos.X += vel.VX * deltaTime
		pos.Y += vel.VY * deltaTime

		// Применяем трение
		vel.VX *= vel.Friction
		vel.VY *= vel.Friction

		// Останавливаем если очень медленно
		if abs(vel.VX) < 0.01 {
			vel.VX = 0
		}
		if abs(vel.VY) < 0.01 {
			vel.VY = 0
		}

		pos.IsMoving = vel.VX != 0 || vel.VY != 0

		// Расчёт угла поворота
		if vel.VX != 0 || vel.VY != 0 {
			pos.Rotation = atan2(vel.VY, vel.VX)
		}
	}
}

// HealthSystem обрабатывает здоровье и регенерацию
type HealthSystem struct {
	BaseSystem
}

func (h *HealthSystem) Update(world *World, deltaTime float32) {
	entities := world.Query(ComponentHealth)

	for _, id := range entities {
		healthComp := world.GetComponent(id, ComponentHealth)
		if healthComp == nil {
			continue
		}

		health := healthComp.Data.(*HealthComponent)

		if health.IsDead {
			continue
		}

		// Регенерация
		if health.Current < health.Max && health.Regen > 0 {
			health.Current += health.Regen * deltaTime
			if health.Current > health.Max {
				health.Current = health.Max
			}
		}

		// Проверка смерти
		if health.Current <= 0 {
			health.Current = 0
			health.IsDead = true
		}
	}
}

// CombatSystem обрабатывает бой
type CombatSystem struct {
	BaseSystem
}

func (c *CombatSystem) Update(world *World, deltaTime float32) {
	entities := world.Query(ComponentCombat)

	currentTime := getCurrentTime()

	for _, id := range entities {
		combatComp := world.GetComponent(id, ComponentCombat)
		if combatComp == nil {
			continue
		}

		combat := combatComp.Data.(*CombatComponent)

		// Перезарядка
		if combat.IsReloading {
			if currentTime-combat.LastAttack >= combat.ReloadTime {
				combat.IsReloading = false
				combat.Ammo = combat.MaxAmmo
			}
		}
	}
}

// RenderSystem подготавливает данные для рендеринга
type RenderSystem struct {
	BaseSystem
	VisibleEntities []EntityID
}

func (r *RenderSystem) Update(world *World, deltaTime float32) {
	entities := world.Query(ComponentPosition, ComponentRender)

	r.VisibleEntities = r.VisibleEntities[:0]

	for _, id := range entities {
		renderComp := world.GetComponent(id, ComponentRender)
		if renderComp == nil {
			continue
		}

		render := renderComp.Data.(*RenderComponent)

		if render.Visible {
			r.VisibleEntities = append(r.VisibleEntities, id)
		}
	}
}

// Update вызывает все системы
func (w *World) Update(deltaTime float32) {
	for _, system := range w.Systems {
		system.Update(w, deltaTime)
	}
}

// RegisterSystem регистрирует систему
func (w *World) RegisterSystem(system System) {
	system.Init(w)
	w.Systems = append(w.Systems, system)
}

// SetEntityCreatedCallback устанавливает callback при создании сущности
func (w *World) SetEntityCreatedCallback(fn func(EntityID)) {
	w.onEntityCreated = fn
}

// SetEntityDestroyedCallback устанавливает callback при удалении сущности
func (w *World) SetEntityDestroyedCallback(fn func(EntityID)) {
	w.onEntityDestroyed = fn
}

// GetStats возвращает статистику мира
func (w *World) GetStats() WorldStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	componentCounts := make([]int, ComponentMax)
	for i := range componentCounts {
		componentCounts[i] = len(w.entitiesByComponent[i])
	}

	return WorldStats{
		TotalEntities:   w.EntityCount,
		ComponentCounts: componentCounts,
	}
}

// WorldStats содержит статистику мира
type WorldStats struct {
	TotalEntities   int
	ComponentCounts []int
}

// Вспомогательные функции
func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func atan2(y, x float32) float32 {
	// Упрощённая реализация, в продакшене использовать math.Atan2
	if x > 0 {
		return y / x
	}
	return 0
}

func getCurrentTime() int64 {
	// В продакшене использовать time.Now().UnixNano()
	return 0
}

// Helper functions for creating entities with common component sets

// CreatePlayer создаёт сущность игрока со всеми необходимыми компонентами
func CreatePlayer(world *World, x, y float32, nickname string) EntityID {
	id := world.CreateEntity()

	// Position
	world.AddComponent(id, ComponentPosition, &PositionComponent{
		X:        x,
		Y:        y,
		Rotation: 0,
		Layer:    1,
	})

	// Velocity
	world.AddComponent(id, ComponentVelocity, &VelocityComponent{
		VX:           0,
		VY:           0,
		Speed:        100,
		MaxSpeed:     200,
		Acceleration: 500,
		Friction:     0.9,
	})

	// Health
	world.AddComponent(id, ComponentHealth, &HealthComponent{
		Current: 100,
		Max:     100,
		Regen:   1,
		Armor:   0,
	})

	// Render
	world.AddComponent(id, ComponentRender, &RenderComponent{
		SpriteID:  1,
		Color:     0xFFFFFFFF,
		Scale:     1,
		Visible:   true,
		Priority:  10,
	})

	// Inventory
	world.AddComponent(id, ComponentInventory, &InventoryComponent{
		Slots:        make([]uint32, 10),
		SelectedSlot: 0,
		Capacity:     10,
		MaxWeight:    50,
	})

	// Combat
	world.AddComponent(id, ComponentCombat, &CombatComponent{
		Damage:    10,
		Range:     100,
		FireRate:  2,
		Ammo:      30,
		MaxAmmo:   30,
	})

	return id
}

// CreateItem создаёт предмет на земле
func CreateItem(world *World, x, y float32, itemID uint32) EntityID {
	id := world.CreateEntity()

	world.AddComponent(id, ComponentPosition, &PositionComponent{
		X:     x,
		Y:     y,
		Layer: 0,
	})

	world.AddComponent(id, ComponentRender, &RenderComponent{
		SpriteID: uint16(itemID),
		Scale:    0.5,
		Visible:  true,
		Priority: 1,
	})

	return id
}

// CreateBuilding создаёт постройку
func CreateBuilding(world *World, x, y float32, buildingID uint16, health float32) EntityID {
	id := world.CreateEntity()

	world.AddComponent(id, ComponentPosition, &PositionComponent{
		X:     x,
		Y:     y,
		Layer: 1,
	})

	world.AddComponent(id, ComponentHealth, &HealthComponent{
		Current: health,
		Max:     health,
	})

	world.AddComponent(id, ComponentRender, &RenderComponent{
		SpriteID: buildingID,
		Scale:    1,
		Visible:  true,
		Priority: 5,
	})

	world.AddComponent(id, ComponentBuild, &BuildComponent{
		BlueprintID:    buildingID,
		BuildHealth:    health,
		MaxBuildHealth: health,
	})

	return id
}
