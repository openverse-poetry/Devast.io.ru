// Package grid реализует пространственное разбиение (Spatial Partitioning) на основе сетки.
// Это критически важно для оптимизации передачи данных в MMO играх.
package grid

import (
	"sync"

	"devast-io-server/internal/ecs"
)

// Cell представляет одну ячейку сетки
type Cell struct {
	ID         int
	X, Y       int32 // Координаты ячейки в сетке
	Entities   []ecs.EntityID // IDs сущностей в этой ячейке
	EntitySet  map[ecs.EntityID]bool // Быстрая проверка наличия
	mu         sync.RWMutex
}

// NewCell создаёт новую ячейку
func NewCell(id int, x, y int32) *Cell {
	return &Cell{
		ID:        id,
		X:         x,
		Y:         y,
		Entities:  make([]ecs.EntityID, 0, 16),
		EntitySet: make(map[ecs.EntityID]bool),
	}
}

// AddEntity добавляет сущность в ячейку
func (c *Cell) AddEntity(entityID ecs.EntityID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.EntitySet[entityID] {
		return // Уже есть
	}

	c.Entities = append(c.Entities, entityID)
	c.EntitySet[entityID] = true
}

// RemoveEntity удаляет сущность из ячейки
func (c *Cell) RemoveEntity(entityID ecs.EntityID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.EntitySet[entityID] {
		return
	}

	// Удаляем из slice
	for i, id := range c.Entities {
		if id == entityID {
			c.Entities[i] = c.Entities[len(c.Entities)-1]
			c.Entities = c.Entities[:len(c.Entities)-1]
			break
		}
	}

	delete(c.EntitySet, entityID)
}

// GetEntities возвращает копию списка сущностей
func (c *Cell) GetEntities() []ecs.EntityID {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ecs.EntityID, len(c.Entities))
	copy(result, c.Entities)
	return result
}

// Count возвращает количество сущностей в ячейке
func (c *Cell) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.Entities)
}

// Grid представляет всю сетку пространственного разбиения
type Grid struct {
	Cells       map[int]*Cell // Map cell ID -> Cell
	CellSize    int32         // Размер ячейки в единицах мира
	WorldWidth  int32         // Ширина мира
	WorldHeight int32         // Высота мира
	Rows, Cols  int32         // Количество ячеек по осям
	mu          sync.RWMutex

	// Кэш позиций сущностей для быстрого поиска их текущей ячейки
	entityPositions map[ecs.EntityID]int // entityID -> cellID
	posMu           sync.RWMutex
}

// NewGrid создаёт новую сетку
func NewGrid(worldWidth, worldHeight, cellSize int32) *Grid {
	cols := (worldWidth + cellSize - 1) / cellSize
	rows := (worldHeight + cellSize - 1) / cellSize

	totalCells := int(cols * rows)

	grid := &Grid{
		Cells:           make(map[int]*Cell, totalCells),
		CellSize:        cellSize,
		WorldWidth:      worldWidth,
		WorldHeight:     worldHeight,
		Cols:            cols,
		Rows:            rows,
		entityPositions: make(map[ecs.EntityID]int, 10000),
	}

	// Предварительно создаём все ячейки
	for y := int32(0); y < rows; y++ {
		for x := int32(0); x < cols; x++ {
			cellID := int(y*cols + x)
			grid.Cells[cellID] = NewCell(cellID, x, y)
		}
	}

	return grid
}

// WorldToGrid преобразует координаты мира в координаты сетки
func (g *Grid) WorldToGrid(worldX, worldY float32) (int32, int32) {
	gridX := int32(worldX) / g.CellSize
	gridY := int32(worldY) / g.CellSize

	// Ограничиваем границами мира
	if gridX < 0 {
		gridX = 0
	} else if gridX >= g.Cols {
		gridX = g.Cols - 1
	}

	if gridY < 0 {
		gridY = 0
	} else if gridY >= g.Rows {
		gridY = g.Rows - 1
	}

	return gridX, gridY
}

// GetCellID возвращает ID ячейки по координатам мира
func (g *Grid) GetCellID(worldX, worldY float32) int {
	gridX, gridY := g.WorldToGrid(worldX, worldY)
	return int(gridY*g.Cols + gridX)
}

// AddEntity добавляет сущность в сетку по её координатам
func (g *Grid) AddEntity(entityID ecs.EntityID, worldX, worldY float32) {
	cellID := g.GetCellID(worldX, worldY)

	g.mu.RLock()
	cell, exists := g.Cells[cellID]
	g.mu.RUnlock()

	if !exists {
		return
	}

	cell.AddEntity(entityID)

	g.posMu.Lock()
	g.entityPositions[entityID] = cellID
	g.posMu.Unlock()
}

// RemoveEntity удаляет сущность из сетки
func (g *Grid) RemoveEntity(entityID ecs.EntityID) {
	g.posMu.RLock()
	cellID, exists := g.entityPositions[ecs.EntityID(entityID)]
	g.posMu.RUnlock()

	if !exists {
		return
	}

	g.mu.RLock()
	cell, exists := g.Cells[cellID]
	g.mu.RUnlock()

	if exists {
		cell.RemoveEntity(entityID)
	}

	g.posMu.Lock()
	delete(g.entityPositions, entityID)
	g.posMu.Unlock()
}

// UpdateEntity обновляет позицию сущности (перемещает между ячейками при необходимости)
func (g *Grid) UpdateEntity(entityID ecs.EntityID, worldX, worldY float32) {
	newCellID := g.GetCellID(worldX, worldY)

	g.posMu.RLock()
	oldCellID, exists := g.entityPositions[entityID]
	g.posMu.RUnlock()

	if !exists {
		// Сущность ещё не в сетке, добавляем
		g.AddEntity(entityID, worldX, worldY)
		return
	}

	if oldCellID == newCellID {
		// Осталась в той же ячейке
		return
	}

	// Перемещаем в новую ячейку
	g.RemoveEntity(entityID)
	g.AddEntity(entityID, worldX, worldY)
}

// GetCell возвращает ячейку по ID
func (g *Grid) GetCell(cellID int) *Cell {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.Cells[cellID]
}

// GetNearbyCells возвращает ячейки в радиусе viewRadius от данной ячейки
// Включая саму ячейку и все соседние в квадрате (2*radius+1)×(2*radius+1)
func (g *Grid) GetNearbyCells(centerX, centerY int32, viewRadius int32) []*Cell {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*Cell

	minX := centerX - viewRadius
	maxX := centerX + viewRadius
	minY := centerY - viewRadius
	maxY := centerY + viewRadius

	// Ограничиваем границами сетки
	if minX < 0 {
		minX = 0
	}
	if maxX >= g.Cols {
		maxX = g.Cols - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= g.Rows {
		maxY = g.Rows - 1
	}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			cellID := int(y*g.Cols + x)
			if cell, exists := g.Cells[cellID]; exists {
				result = append(result, cell)
			}
		}
	}

	return result
}

// GetVisibleEntities возвращает все сущности в радиусе видимости от точки
// Это основной метод для определения того, какие обновления отправлять игроку
func (g *Grid) GetVisibleEntities(worldX, worldY float32, viewRadiusCells int32) []ecs.EntityID {
	gridX, gridY := g.WorldToGrid(worldX, worldY)
	nearbyCells := g.GetNearbyCells(gridX, gridY, viewRadiusCells)

	// Используем map для избежания дубликатов
	entitySet := make(map[uint32]bool)

	for _, cell := range nearbyCells {
		entities := cell.GetEntities()
		for _, entityID := range entities {
			entitySet[uint32(entityID)] = true
		}
	}

	// Преобразуем в slice
	result := make([]ecs.EntityID, 0, len(entitySet))
	for entityID := range entitySet {
		result = append(result, ecs.EntityID(entityID))
	}

	return result
}

// GetEntityCellID возвращает ID ячейки, в которой находится сущность
func (g *Grid) GetEntityCellID(entityID uint32) (int, bool) {
	g.posMu.RLock()
	defer g.posMu.RUnlock()

	cellID, exists := g.entityPositions[ecs.EntityID(entityID)]
	return cellID, exists
}

// GetStats возвращает статистику по сетке
func (g *Grid) GetStats() GridStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	totalEntities := 0
	maxEntities := 0
	var maxCellID int

	for cellID, cell := range g.Cells {
		count := cell.Count()
		totalEntities += count
		if count > maxEntities {
			maxEntities = count
			maxCellID = cellID
		}
	}

	return GridStats{
		TotalCells:    len(g.Cells),
		TotalEntities: totalEntities,
		MaxEntities:   maxEntities,
		MaxCellID:     maxCellID,
		AvgPerCell:    float32(totalEntities) / float32(len(g.Cells)),
	}
}

// GridStats содержит статистику сетки
type GridStats struct {
	TotalCells    int
	TotalEntities int
	MaxEntities   int
	MaxCellID     int
	AvgPerCell    float32
}

// QueryRadius возвращает сущности в точном круговом радиусе (более точный, но медленный)
func (g *Grid) QueryRadius(centerX, centerY, radius float32) []ecs.EntityID {
	radiusCells := int32(radius/float32(g.CellSize)) + 1
	gridX, gridY := g.WorldToGrid(centerX, centerY)
	nearbyCells := g.GetNearbyCells(gridX, gridY, radiusCells)

	entitySet := make(map[uint32]bool)

	for _, cell := range nearbyCells {
		entities := cell.GetEntities()
		for _, entityID := range entities {
			// Здесь нужна дополнительная проверка расстояния
			// Для этого нужен доступ к позициям сущностей через ECS
			// Пока просто добавляем все из ячеек
			entitySet[uint32(entityID)] = true
		}
	}

	result := make([]ecs.EntityID, 0, len(entitySet))
	for entityID := range entitySet {
		result = append(result, ecs.EntityID(entityID))
	}

	return result
}

// ForEachEntityInCell применяет функцию ко всем сущностям в ячейке
func (g *Grid) ForEachEntityInCell(cellID int, fn func(entityID ecs.EntityID) bool) {
	g.mu.RLock()
	cell, exists := g.Cells[cellID]
	g.mu.RUnlock()

	if !exists {
		return
	}

	cell.mu.RLock()
	defer cell.mu.RUnlock()

	for _, entityID := range cell.Entities {
		if !fn(entityID) {
			break
		}
	}
}

// Clear очищает всю сетку
func (g *Grid) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, cell := range g.Cells {
		cell.mu.Lock()
		cell.Entities = cell.Entities[:0]
		// Очищаем map вручную для совместимости со старыми версиями Go
		for k := range cell.EntitySet {
			delete(cell.EntitySet, k)
		}
		cell.mu.Unlock()
	}

	g.posMu.Lock()
	// Очищаем map вручную для совместимости со старыми версиями Go
	for k := range g.entityPositions {
		delete(g.entityPositions, k)
	}
	g.posMu.Unlock()
}
