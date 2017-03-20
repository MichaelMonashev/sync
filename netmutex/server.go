package netmutex

import (
	"crypto/rand"
	"net"
	"sync/atomic"
	"time"
)

type server struct {
	id      uint64
	addr    string
	conn    *net.UDPConn // ToDo под виндой в Go нельзя делать одновременно несколько recv() с одним комменктом(дескриптором). Можно попробовать создать несколько коннектов, може тогда будет отправляться в сумме больше запросов.
	frameID []byte       // ID соединения (случайное 64-битное число, генерится клиентом)
	seqID   uint64       // последовательный номер пакета в соединении
	fails   uint32
	busy    int32
	//	bufCh                chan *acc
	lastReadDeadlineTime time.Time
}

//// работает через conn.Write()
//func (s *server) run1(done chan struct{}) {
//	bb := make([]*acc, 0)
//	bigBuf := make([]byte, 0, 65536)
//
//	for {
//		select {
//		case <-done:
//			return
//
//		case b := <-s.bufCh:
//			bb = append(bb, b)
//			bigBuf = append(bigBuf, b.buf...)
//
//			// вычитываем оставшиеся буферы
//			for len(bigBuf) < 32500 { // 65000/2 ToDo. Переписать. При таком ограничении буфер не используется на полную
//				select {
//				case b = <-s.bufCh:
//					bb = append(bb, b)
//					bigBuf = append(bigBuf, b.buf...)
//					continue
//				default:
//				}
//				break
//			}
//
//			_, err := s.conn.Write(bigBuf)
//			bigBuf = bigBuf[:0]
//
//			// рассылаем ошибки
//			for _, b = range bb {
//				b.err <- err
//			}
//
//			bb = bb[:0]
//		}
//	}
//}
//
//// работает через Buffers.WriteTo() . Чуть медленее предыдущего варианта
//func (s *server) run(done chan struct{}) {
//	bb := make([]*acc, 0)
//	nb := net.Buffers{}
//
//	for {
//		select {
//		case <-done:
//			return
//
//		case b := <-s.bufCh:
//
//			// _, err := s.conn.Write(b.buf)
//			// b.err <- err
//
//			bb = append(bb, b)
//			nb = append(nb, b.buf)
//			l := len(b.buf)
//
//			// вычитываем оставшиеся буферы
//			for l < 32500 { // 65000/2 ToDo. Переписать. При таком ограничении буфер не используется на полную
//				select {
//				case b = <-s.bufCh:
//					bb = append(bb, b)
//					nb = append(nb, b.buf)
//					l += len(b.buf)
//					continue
//				default:
//				}
//				break
//			}
//
//			// отправляем буферы серверу
//			_, err := nb.WriteTo(s.conn)
//			//warn(n, err, len(nb))
//			if err != nil {
//				warn("Error while Buffers.WriteTo():", err)
//				s.fail()
//			}
//
//			// рассылаем ошибки
//			for _, b = range bb {
//				b.err <- err
//			}
//
//			bb = bb[:0]
//			nb = nb[:0] // WriteTo() удаляет отправленные данные, а неотправленные удаляем мы
//		}
//	}
//}

func (server *server) fail() {
	atomic.AddUint32(&server.fails, 1)
}

func (server *server) ok() {
	atomic.StoreUint32(&server.fails, 0)
}

// возвращает следующий номер пакета в соединении
func (server *server) getSeqID() uint64 {
	return atomic.AddUint64(&server.seqID, 1)
}

// генерит случайное ID соединения
func genFrameID() ([]byte, error) {

	buf := make([]byte, 8)

	_, err := rand.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}
