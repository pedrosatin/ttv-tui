package player

const (
	// maxSixelFrame bounds the memory one frame can take. Pure noise, the worst
	// case for the encoder, measured 3.3 MiB at 1280x720, what a panel of 18 rows
	// reaches with 40px cells; anything larger is not a frame this app asked for.
	maxSixelFrame = 8 << 20
	// maxSixelLine matches the limit consumeLines gives streamlink's lines.
	maxSixelLine = 1 << 20

	asciiESC = 0x1b
)

type sixelStreamState int

const (
	streamGround sixelStreamState = iota
	streamEscape
	streamCSI
	streamDCS
	streamDCSEscape
	// streamSkipDCS swallows a rejected DCS up to its terminator.
	streamSkipDCS
	streamSkipDCSEscape
)

// sixelDemux splits the player's stdout. mpv writes sixel frames there along
// with cursor, mouse and position sequences meant for a terminal it does not own
// here. Only whole sixel DCS sequences leave as frames and everything that
// reaches the terminal goes through onFrame, so the player cannot move the
// cursor or turn on mouse reporting. Plain text leaves as lines.
type sixelDemux struct {
	onLine  func(string)
	onFrame func([]byte)

	state sixelStreamState
	line  []byte
	frame []byte
}

// feed consumes one chunk. Frames and lines can span chunks.
func (d *sixelDemux) feed(chunk []byte) {
	for _, b := range chunk {
		d.step(b)
	}
}

// flush delivers a last line without a newline. An unterminated frame is dropped.
func (d *sixelDemux) flush() {
	if len(d.line) > 0 {
		d.endLine()
	}
	d.frame = nil
	d.state = streamGround
}

func (d *sixelDemux) step(b byte) {
	switch d.state {
	case streamGround:
		d.ground(b)
	case streamEscape:
		d.escape(b)
	case streamCSI:
		if b >= 0x40 && b <= 0x7e {
			d.state = streamGround
		}
	case streamDCS:
		d.dcs(b)
	case streamDCSEscape:
		if b == '\\' {
			d.endFrame()
			return
		}
		// ESC cancels a DCS: what was collected is not a frame.
		d.frame = nil
		d.state = streamEscape
		d.escape(b)
	case streamSkipDCS:
		if b == asciiESC {
			d.state = streamSkipDCSEscape
		}
	case streamSkipDCSEscape:
		switch b {
		case '\\':
			d.state = streamGround
		case asciiESC:
		default:
			d.state = streamSkipDCS
		}
	}
}

func (d *sixelDemux) ground(b byte) {
	switch {
	case b == asciiESC:
		d.state = streamEscape
	case b == '\n':
		d.endLine()
	case b < 0x20 && b != '\t', b == 0x7f:
		// mpv pads each frame with a NUL, and a line ending in \r is still a line.
	default:
		d.line = append(d.line, b)
		if len(d.line) >= maxSixelLine {
			d.endLine()
		}
	}
}

func (d *sixelDemux) escape(b byte) {
	switch b {
	case 'P':
		d.frame = append(make([]byte, 0, 256*1024), asciiESC, 'P')
		d.state = streamDCS
	case '[':
		d.state = streamCSI
	case asciiESC:
	default:
		d.state = streamGround
	}
}

func (d *sixelDemux) dcs(b byte) {
	switch {
	case b == asciiESC:
		d.state = streamDCSEscape
	case (b < 0x20 && b != '\r' && b != '\n') || b >= 0x7f, len(d.frame) >= maxSixelFrame:
		// CAN, SUB and raw C1 end a DCS on some terminals and not on others.
		// Nothing libsixel writes needs them, so such a frame is not forwarded.
		d.frame = nil
		d.state = streamSkipDCS
	default:
		d.frame = append(d.frame, b)
	}
}

func (d *sixelDemux) endFrame() {
	frame := append(d.frame, asciiESC, '\\')
	d.frame = nil
	d.state = streamGround
	if d.onFrame != nil {
		d.onFrame(frame)
	}
}

func (d *sixelDemux) endLine() {
	line := string(d.line)
	d.line = d.line[:0]
	if line != "" && d.onLine != nil {
		d.onLine(line)
	}
}

// sixelRaster reads the pixel size a frame declares in its raster attributes
// ("Pan;Pad;Ph;Pv). ok is false for anything but a sixel DCS that declares it, so
// a device control string with another purpose (DECRQSS, XTGETTCAP) never reaches
// the terminal, whose reply would land in the keyboard input.
func sixelRaster(frame []byte) (width, height int, ok bool) {
	if len(frame) < 4 || frame[0] != asciiESC || frame[1] != 'P' {
		return 0, 0, false
	}
	i := 2
	for i < len(frame) && (frame[i] == ';' || (frame[i] >= '0' && frame[i] <= '9')) {
		i++
	}
	if i+1 >= len(frame) || frame[i] != 'q' || frame[i+1] != '"' {
		return 0, 0, false
	}
	i += 2
	var params [4]int
	for p := range params {
		digits := 0
		for i < len(frame) && frame[i] >= '0' && frame[i] <= '9' && digits < 6 {
			params[p] = params[p]*10 + int(frame[i]-'0')
			i++
			digits++
		}
		if digits == 0 {
			return 0, 0, false
		}
		if p < len(params)-1 {
			if i >= len(frame) || frame[i] != ';' {
				return 0, 0, false
			}
			i++
		}
	}
	return params[2], params[3], params[2] > 0 && params[3] > 0
}
