package netmutex

import (
	"bytes"
	"encoding/binary"

	"github.com/MichaelMonashev/sync/netmutex/checksum"
	"github.com/MichaelMonashev/sync/netmutex/code"
)

type response struct {
	id          commandID         // уникальный номер команды
	servers     map[uint64]string // список серверов при OPTIONS
	serverID    uint64            // номер сервера при REDIRECT
	description string            // описание ошибки при ERROR
	code        byte              // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
}

func (resp *response) unmarshalPacket(buf []byte) (bool, error) {

	busy := false

	// проверка на корректность длины буфера
	if len(buf) < protocolHeaderSize+protocolTailSize {
		return busy, errWrongResponse
	}

	// проверка на версию протокола. Обрабатываем только первую версию.
	if buf[0] != 1 {
		return busy, errWrongResponse
	}

	// перегружен ли сервер
	if buf[2]&code.BUSY > 0 {
		busy = true
	}

	// пока принимаем только одиночные пакеты
	if buf[2]&code.FRAGMENTED > 0 {
		return busy, errWrongResponse
	}

	// проверяем длину пакета
	size := binary.LittleEndian.Uint64(buf[8:])
	if size != uint64(len(buf)) {
		return busy, errWrongResponse
	}

	// проверяем контрольную сумму пакета
	calculatedChecksum := checksum.Checksum(buf[:len(buf)-protocolTailSize])
	chunkChecksum := buf[len(buf)-protocolTailSize:]
	if !bytes.Equal(calculatedChecksum[:], chunkChecksum) {
		return busy, errWrongResponse
	}

	// из данных пакета формируем объект ответа
	return busy, resp.unmarshalCommand(buf[protocolHeaderSize : len(buf)-protocolTailSize])
}

func (resp *response) unmarshalCommand(buf []byte) error {

	if len(buf) < 25 {
		return errWrongResponse //Errorln("wrong data size", len(buf))
	}

	resp.code = buf[0] //  0 байт - команда: OPTIONS, OK, и т.д.
	switch resp.code {
	case code.OPTIONS:

		resp.id.serverID = binary.LittleEndian.Uint64(buf[1:])
		resp.id.connectionID = binary.LittleEndian.Uint64(buf[9:])

		nubmerOfServers := binary.LittleEndian.Uint64(buf[17:])

		if nubmerOfServers < 1 || nubmerOfServers > 7 {
			return errWrongResponse //Errorln("wrong number of servers", serversNum, "in OPTIONS")
		}

		pos := 25
		for i := nubmerOfServers; i > 0; i-- {

			//проверяем, что pos не выйдет за границы buf
			if (pos + 8 + 1) >= len(buf) {
				return errWrongResponse //Errorln("wrong servers position", serversPos, "in OPTIONS")
			}
			serverID := binary.LittleEndian.Uint64(buf[pos:])
			pos += 8

			serverStringSize := int(buf[pos])
			pos++

			//проверяем, что pos не выходит за границы buf
			if (pos + serverStringSize) > len(buf) {
				return errWrongResponse //Errorln("wrong servers string size: from", pos, "to", (pos + serverStringSize), "in OPTIONS")
			}

			serverString := string(buf[pos : pos+serverStringSize])
			pos += serverStringSize

			resp.servers[serverID] = serverString
		}
		if pos != len(buf) {
			return errWrongResponse //Errorln("wrong packet size", bufSize, "in OPTIONS")
		}

	case code.OK, code.DISCONNECTED, code.ISOLATED, code.LOCKED:
		if len(buf) != 25 {
			return errWrongResponse //Errorln("wrong packet size", bufSize, "in OK, DISCONNECTED, ISOLATED or LOCKED.")
		}
		resp.id.serverID = binary.LittleEndian.Uint64(buf[1:])
		resp.id.connectionID = binary.LittleEndian.Uint64(buf[9:])
		resp.id.requestID = binary.LittleEndian.Uint64(buf[17:])

	case code.REDIRECT:
		if len(buf) != 33 {
			return errWrongResponse //Errorln("wrong packet size", bufSize, "in REDIRECT")
		}
		resp.id.serverID = binary.LittleEndian.Uint64(buf[1:])
		resp.id.connectionID = binary.LittleEndian.Uint64(buf[9:])
		resp.id.requestID = binary.LittleEndian.Uint64(buf[17:])
		resp.serverID = binary.LittleEndian.Uint64(buf[25:])

	case code.ERROR:
		if len(buf) < 25 {
			return errWrongResponse //Errorln("wrong packet size", bufSize, "in ERROR")
		}
		resp.id.serverID = binary.LittleEndian.Uint64(buf[1:])
		resp.id.connectionID = binary.LittleEndian.Uint64(buf[9:])
		resp.id.requestID = binary.LittleEndian.Uint64(buf[17:])

		descStringSize := int(buf[25]) // 32 байт - размер текста ошибки
		if descStringSize+26 != len(buf) {
			return errWrongResponse //Errorln("wrong description size", descStringSize, bufSize, "in ERROR")
		}

		resp.description = string(buf[26 : 26+descStringSize])

	default:
		return errWrongResponse //Errorln("wrong command code:", fmt.Sprint(resp.code))
	}

	return nil
}
