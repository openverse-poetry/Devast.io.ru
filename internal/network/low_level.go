//go:build linux
// +build linux

package network

import (
	"net"
	"syscall"
	"unsafe"
)

// LowLevelSender предоставляет методы для отправки данных с минимальными накладными расходами.
// Вдохновлено принципами GoodbyeDPI: обход лишних копирований и работа ближе к железу.
type LowLevelSender struct {
	fd int
}

// NewLowLevelSender инициализирует отправщик, извлекая файловый дескриптор из TCP-соединения.
// Это позволяет нам использовать прямые системные вызовы (syscalls), минуя часть логики net.Poller.
func NewLowLevelSender(conn net.Conn) (*LowLevelSender, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, ErrNotTCPConnection
	}

	// Получаем сырой файловый дескриптор.
	// Внимание: После вызова File(), управление дескриптором переходит к вызывающему,
	// но для net.Conn в Go это безопасно, пока мы не закрываем сам дескриптор вручную.
	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil, err
	}

	var fd int
	ctrlErr := rawConn.Control(func(f uintptr) {
		fd = int(f)
	})
	if ctrlErr != nil {
		return nil, ctrlErr
	}

	return &LowLevelSender{fd: fd}, nil
}

// SendVec выполняет векторизированную запись (writev).
// Это аналог "нулевого копирования" для данных в памяти.
// Вместо того чтобы склеивать заголовок (например, длину пакета) и тело (игровые данные)
// в один большой буфер (что создает нагрузку на аллокатор и GC), мы передаем указатели
// на два разных участка памяти прямо в ядро ОС. Ядро само соберет пакет для сетевой карты.
//
// Это критически важно для 50 000 CCU, так как снижает количество инструкций CPU на пакет.
func (s *LowLevelSender) SendVec(header []byte, payload []byte) (int, error) {
	if len(header) == 0 && len(payload) == 0 {
		return 0, nil
	}

	// Подготавливаем структуру iovec для системного вызова writev
	// iovec содержит массив структур {base, len}
	var iov []syscall.Iovec

	if len(header) > 0 {
		iov = append(iov, syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(&header[0])),
			Len:  uint64(len(header)),
		})
	}

	if len(payload) > 0 {
		iov = append(iov, syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(&payload[0])),
			Len:  uint64(len(payload)),
		})
	}

	// Прямой системный вызов writev(2)
	// Данные отправляются в сокет одним атомарным действием без промежуточного копирования в user-space буферы
	n, _, errno := syscall.Syscall(syscall.SYS_WRITEV, uintptr(s.fd), uintptr(unsafe.Pointer(&iov[0])), uintptr(len(iov)))
	if errno != 0 {
		return int(n), errno
	}

	return int(n), nil
}

// SendBuf оптимизированная отправка одного непрерывного буфера.
// Используется, если данные уже собраны (например, сериализованный FlatBuffer).
// Прямой вызов write вместо net.Conn.Write для исключения проверок дедлайнов и мьютексов net.Poller,
// если мы хотим максимальной скорости в рамках одного горутин-цикла.
func (s *LowLevelSender) SendBuf(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	n, _, errno := syscall.Syscall(syscall.SYS_WRITE, uintptr(s.fd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 {
		return int(n), errno
	}

	return int(n), nil
}

// SetTCPNoDelay отключает алгоритм Нейгла, гарантируя мгновенную отправку пакетов.
// Для экшн-игр (IO games) задержка важнее пропускной способности.
func (s *LowLevelSender) SetTCPNoDelay() error {
	// Используем setsockopt напрямую
	// TCP_NODELAY = 1
	errno := syscall.SetsockoptInt(s.fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
	if errno != nil {
		return errno
	}
	return nil
}

// SetSendBufferSize устанавливает размер буфера отправки ядра.
// Увеличение буфера может помочь при пиковых нагрузках, предотвращая блокировку записи.
func (s *LowLevelSender) SetSendBufferSize(size int) error {
	errno := syscall.SetsockoptInt(s.fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, size)
	if errno != nil {
		return errno
	}
	return nil
}

// Ошибка соединения не TCP
var ErrNotTCPConnection = syscall.EINVAL

// GetFD возвращает сырой файловый дескриптор для внешних операций (epoll, io_uring и т.д.)
func (s *LowLevelSender) GetFD() int {
	return s.fd
}
