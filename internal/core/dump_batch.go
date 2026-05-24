package core

const dumpIDChunkSize = 5000

func appendDumpIDs(buffer []string, ids []string, flush func([]string) error) ([]string, bool, error) {
	wroteAny := false
	for len(ids) > 0 {
		space := min(dumpIDChunkSize-len(buffer), len(ids))
		buffer = append(buffer, ids[:space]...)
		ids = ids[space:]
		if len(buffer) == dumpIDChunkSize {
			if err := flush(buffer); err != nil {
				return buffer, wroteAny, err
			}
			wroteAny = true
			buffer = buffer[:0]
		}
	}
	return buffer, wroteAny, nil
}

func flushDumpIDs(buffer []string, flush func([]string) error) (bool, error) {
	if len(buffer) == 0 {
		return false, nil
	}
	return true, flush(buffer)
}
