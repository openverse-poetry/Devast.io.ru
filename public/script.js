// Devast.io Client - JavaScript WebSocket Client
// Подключается к Go серверу через WebSocket

class GameClient {
    constructor() {
        this.ws = null;
        this.isConnected = false;
        this.playerId = null;
        this.nickname = '';
        this.canvas = document.getElementById('gameCanvas');
        this.ctx = this.canvas.getContext('2d');
        this.minimapCanvas = document.getElementById('minimap');
        this.minimapCtx = this.minimapCanvas.getContext('2d');
        
        // Игровое состояние
        this.players = new Map();
        this.entities = [];
        this.worldSize = 10000;
        this.camera = { x: 0, y: 0 };
        this.lastHeartbeat = Date.now();
        
        // Ввод
        this.keys = {};
        this.mouse = { x: 0, y: 0, down: false };
        
        // Packet types (должны совпадать с Go сервером)
        this.PacketType = {
            ClientHello: 0x01,
            ServerWelcome: 0x02,
            MoveRequest: 0x10,
            MoveUpdate: 0x11,
            Ping: 0xFE,
            Pong: 0xFF
        };
        
        this.init();
    }
    
    init() {
        this.resizeCanvas();
        window.addEventListener('resize', () => this.resizeCanvas());
        this.setupInputHandlers();
        this.setupUIHandlers();
        this.gameLoop();
        this.heartbeatLoop();
    }
    
    resizeCanvas() {
        this.canvas.width = window.innerWidth;
        this.canvas.height = window.innerHeight;
    }
    
    setupUIHandlers() {
        const playButton = document.getElementById('playButton');
        const nicknameInput = document.getElementById('nicknameInput');
        
        playButton.addEventListener('click', () => {
            const nickname = nicknameInput.value.trim() || 'Player';
            this.connect(nickname);
        });
        
        nicknameInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                const nickname = nicknameInput.value.trim() || 'Player';
                this.connect(nickname);
            }
        });
    }
    
    setupInputHandlers() {
        // Клавиатура
        window.addEventListener('keydown', (e) => {
            this.keys[e.code] = true;
            
            // Отправляем запрос движения при нажатии клавиш
            if (this.isConnected && ['KeyW', 'KeyA', 'KeyS', 'KeyD'].includes(e.code)) {
                this.sendMoveRequest();
            }
        });
        
        window.addEventListener('keyup', (e) => {
            this.keys[e.code] = false;
        });
        
        // Мышь
        this.canvas.addEventListener('mousemove', (e) => {
            this.mouse.x = e.clientX;
            this.mouse.y = e.clientY;
        });
        
        this.canvas.addEventListener('mousedown', (e) => {
            this.mouse.down = true;
        });
        
        this.canvas.addEventListener('mouseup', (e) => {
            this.mouse.down = false;
        });
    }
    
    connect(nickname) {
        this.nickname = nickname;
        
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;
        
        console.log('[CLIENT] Connecting to:', wsUrl);
        
        try {
            this.ws = new WebSocket(wsUrl);
            this.ws.binaryType = 'arraybuffer';
            
            this.ws.onopen = () => {
                console.log('[CLIENT] Connected to server');
                this.isConnected = true;
                this.updateConnectionStatus(true);
                this.sendClientHello();
            };
            
            this.ws.onclose = () => {
                console.log('[CLIENT] Disconnected from server');
                this.isConnected = false;
                this.updateConnectionStatus(false);
                this.showLoginScreen();
            };
            
            this.ws.onerror = (error) => {
                console.error('[CLIENT] WebSocket error:', error);
                this.updateConnectionStatus(false);
            };
            
            this.ws.onmessage = (event) => {
                this.handleMessage(event.data);
            };
        } catch (error) {
            console.error('[CLIENT] Connection error:', error);
            alert('Не удалось подключиться к серверу!');
        }
    }
    
    disconnect() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
        this.isConnected = false;
    }
    
    sendClientHello() {
        // Формируем пакет ClientHello
        // Заголовок: 1 байт тип + 2 байта длина + 2 байта sequenceID
        const nicknameBytes = new TextEncoder().encode(this.nickname);
        const dataLength = 1 + nicknameBytes.length; // version (1 byte) + nickname
        
        const packet = new Uint8Array(5 + dataLength); // header(5) + data
        
        // Заголовок
        packet[0] = this.PacketType.ClientHello;
        new DataView(packet.buffer).setUint16(1, dataLength, true); // length
        new DataView(packet.buffer).setUint16(3, 0, true); // sequenceID
        
        // Данные
        packet[5] = 1; // версия протокола
        packet.set(nicknameBytes, 6);
        
        console.log('[CLIENT] Sending ClientHello, nickname:', this.nickname);
        this.ws.send(packet);
    }
    
    sendMoveRequest() {
        if (!this.isConnected) return;
        
        // Определяем направление движения
        let dx = 0, dy = 0;
        const speed = 1;
        
        if (this.keys['KeyW']) dy -= speed;
        if (this.keys['KeyS']) dy += speed;
        if (this.keys['KeyA']) dx -= speed;
        if (this.keys['KeyD']) dx += speed;
        
        if (dx === 0 && dy === 0) return;
        
        // Нормализуем диагональное движение
        if (dx !== 0 && dy !== 0) {
            const len = Math.sqrt(dx * dx + dy * dy);
            dx /= len;
            dy /= len;
        }
        
        // Формируем пакет MoveRequest
        const dataLength = 8; // 4 байта dx (float32) + 4 байта dy (float32)
        const packet = new Uint8Array(5 + dataLength);
        
        packet[0] = this.PacketType.MoveRequest;
        new DataView(packet.buffer).setUint16(1, dataLength, true);
        new DataView(packet.buffer).setUint16(3, 0, true);
        
        // Данные (little-endian float32)
        new DataView(packet.buffer).setFloat32(5, dx, true);
        new DataView(packet.buffer).setFloat32(9, dy, true);
        
        this.ws.send(packet);
    }
    
    sendPing() {
        if (!this.isConnected) return;
        
        const packet = new Uint8Array(5);
        packet[0] = this.PacketType.Ping;
        new DataView(packet.buffer).setUint16(1, 0, true);
        new DataView(packet.buffer).setUint16(3, 0, true);
        
        this.ws.send(packet);
    }
    
    handleMessage(data) {
        const buffer = data instanceof ArrayBuffer ? data : data.buffer;
        const view = new DataView(buffer);
        
        if (buffer.byteLength < 5) {
            console.warn('[CLIENT] Invalid packet size');
            return;
        }
        
        const packetType = view.getUint8(0);
        const length = view.getUint16(1, true);
        
        console.log('[CLIENT] Received packet type:', packetType.toString(16), 'length:', length);
        
        switch (packetType) {
            case this.PacketType.ServerWelcome:
                this.handleServerWelcome(view, buffer);
                break;
            case this.PacketType.MoveUpdate:
                this.handleMoveUpdate(view, buffer);
                break;
            case this.PacketType.Pong:
                this.lastHeartbeat = Date.now();
                break;
            default:
                console.log('[CLIENT] Unknown packet type:', packetType);
        }
    }
    
    handleServerWelcome(view, buffer) {
        console.log('[CLIENT] Received ServerWelcome');
        
        // Парсим ServerWelcome
        // PlayerID (4), WorldSeed (4), SpawnX (4), SpawnY (4), TickCount (4), ServerTime (8)
        const playerID = view.getUint32(5, true);
        const worldSeed = view.getInt32(9, true);
        const spawnX = view.getInt32(13, true);
        const spawnY = view.getInt32(17, true);
        
        this.playerId = playerID;
        
        // Обновляем UI
        document.getElementById('playerName').textContent = this.nickname;
        document.getElementById('playerId').textContent = playerID;
        
        // Скрываем экран входа, показываем HUD
        document.getElementById('loginScreen').style.display = 'none';
        document.getElementById('hud').style.display = 'block';
        
        console.log(`[CLIENT] Welcome! PlayerID: ${playerID}, Spawn: (${spawnX}, ${spawnY})`);
        
        // Добавляем своего игрока
        this.players.set(playerID, {
            x: spawnX,
            y: spawnY,
            nickname: this.nickname,
            isSelf: true
        });
    }
    
    handleMoveUpdate(view, buffer) {
        // Парсим обновление позиций игроков
        // В реальном проекте здесь будет более сложная логика
        console.log('[CLIENT] Received MoveUpdate');
    }
    
    updateConnectionStatus(connected) {
        const statusText = document.getElementById('statusText');
        if (connected) {
            statusText.textContent = '● Подключено';
            statusText.className = 'status-connected';
        } else {
            statusText.textContent = '● Отключено';
            statusText.className = 'status-disconnected';
        }
    }
    
    showLoginScreen() {
        document.getElementById('loginScreen').style.display = 'flex';
        document.getElementById('hud').style.display = 'none';
    }
    
    heartbeatLoop() {
        setInterval(() => {
            if (this.isConnected) {
                this.sendPing();
                
                // Проверка таймаута
                if (Date.now() - this.lastHeartbeat > 10000) {
                    console.warn('[CLIENT] Server heartbeat timeout');
                    this.disconnect();
                }
            }
        }, 5000);
    }
    
    gameLoop() {
        this.render();
        requestAnimationFrame(() => this.gameLoop());
    }
    
    render() {
        // Очистка
        this.ctx.fillStyle = '#1a1a2e';
        this.ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);
        
        // Рисуем сетку мира
        this.drawGrid();
        
        // Рисуем игроков
        this.drawPlayers();
        
        // Рисуем миникарту
        this.drawMinimap();
    }
    
    drawGrid() {
        const gridSize = 50;
        const offsetX = -this.camera.x % gridSize;
        const offsetY = -this.camera.y % gridSize;
        
        this.ctx.strokeStyle = '#2d2d44';
        this.ctx.lineWidth = 1;
        
        for (let x = offsetX; x < this.canvas.width; x += gridSize) {
            this.ctx.beginPath();
            this.ctx.moveTo(x, 0);
            this.ctx.lineTo(x, this.canvas.height);
            this.ctx.stroke();
        }
        
        for (let y = offsetY; y < this.canvas.height; y += gridSize) {
            this.ctx.beginPath();
            this.ctx.moveTo(0, y);
            this.ctx.lineTo(this.canvas.width, y);
            this.ctx.stroke();
        }
    }
    
    drawPlayers() {
        const centerX = this.canvas.width / 2;
        const centerY = this.canvas.height / 2;
        
        for (const [id, player] of this.players) {
            const screenX = centerX + (player.x - this.camera.x);
            const screenY = centerY + (player.y - this.camera.y);
            
            // Рисуем игрока
            this.ctx.beginPath();
            this.ctx.arc(screenX, screenY, 20, 0, Math.PI * 2);
            
            if (player.isSelf) {
                this.ctx.fillStyle = '#ffd700';
            } else {
                this.ctx.fillStyle = '#ff6666';
            }
            
            this.ctx.fill();
            this.ctx.strokeStyle = '#fff';
            this.ctx.lineWidth = 2;
            this.ctx.stroke();
            
            // Рисуем никнейм
            this.ctx.fillStyle = '#fff';
            this.ctx.font = '14px Arial';
            this.ctx.textAlign = 'center';
            this.ctx.fillText(player.nickname, screenX, screenY - 30);
        }
    }
    
    drawMinimap() {
        const ctx = this.minimapCtx;
        const width = this.minimapCanvas.width;
        const height = this.minimapCanvas.height;
        
        // Очистка
        ctx.fillStyle = '#1a1a2e';
        ctx.fillRect(0, 0, width, height);
        
        // Рисуем границы мира
        ctx.strokeStyle = '#4a4a6a';
        ctx.lineWidth = 2;
        ctx.strokeRect(0, 0, width, height);
        
        // Рисуем игроков на миникарте
        const scale = Math.min(width, height) / this.worldSize;
        
        for (const [id, player] of this.players) {
            const mapX = width / 2 + player.x * scale;
            const mapY = height / 2 + player.y * scale;
            
            ctx.beginPath();
            ctx.arc(mapX, mapY, player.isSelf ? 5 : 3, 0, Math.PI * 2);
            ctx.fillStyle = player.isSelf ? '#ffd700' : '#ff6666';
            ctx.fill();
        }
    }
}

// Запуск клиента при загрузке страницы
window.addEventListener('DOMContentLoaded', () => {
    console.log('[CLIENT] Devast.io Client initializing...');
    window.game = new GameClient();
});
