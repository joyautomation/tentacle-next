package ladder

import (
	"time"

	"github.com/joyautomation/tentacle/plc"
)

// Element is a single ladder logic element (contact, coil, timer, etc.).
type Element interface {
	evaluate(rungIn bool, s *State) bool
}

// ─── Contacts ────────────────────────────────────────────────────────────────

// noContact is a Normally Open contact: passes rungIn when the tag is true.
type noContact struct{ tag string }

func (e *noContact) evaluate(rungIn bool, s *State) bool {
	return rungIn && s.getTag(e.tag)
}

// NO creates a Normally Open contact element. The rung continues true only
// when the referenced tag is true.
func NO(tag string) Element { return &noContact{tag: tag} }

// ncContact is a Normally Closed contact: passes rungIn when the tag is false.
type ncContact struct{ tag string }

func (e *ncContact) evaluate(rungIn bool, s *State) bool {
	return rungIn && !s.getTag(e.tag)
}

// NC creates a Normally Closed contact element. The rung continues true only
// when the referenced tag is false.
func NC(tag string) Element { return &ncContact{tag: tag} }

// ─── Coils ───────────────────────────────────────────────────────────────────

// oteCoil is an Output Energize coil: sets the tag to match the rung state.
type oteCoil struct{ tag string }

func (e *oteCoil) evaluate(rungIn bool, s *State) bool {
	s.setTag(e.tag, rungIn)
	return rungIn
}

// OTE creates an Output Energize coil. The tag is set to true when the rung
// is true and false when the rung is false.
func OTE(tag string) Element { return &oteCoil{tag: tag} }

// otlCoil is an Output Latch coil: latches the tag true when the rung is true.
type otlCoil struct{ tag string }

func (e *otlCoil) evaluate(rungIn bool, s *State) bool {
	if rungIn {
		s.setTag(e.tag, true)
	}
	return rungIn
}

// OTL creates an Output Latch coil. When the rung is true the tag is set to
// true. The tag remains true until explicitly unlatched by OTU.
func OTL(tag string) Element { return &otlCoil{tag: tag} }

// otuCoil is an Output Unlatch coil: clears the tag when the rung is true.
type otuCoil struct{ tag string }

func (e *otuCoil) evaluate(rungIn bool, s *State) bool {
	if rungIn {
		s.setTag(e.tag, false)
	}
	return rungIn
}

// OTU creates an Output Unlatch coil. When the rung is true the tag is set
// to false.
func OTU(tag string) Element { return &otuCoil{tag: tag} }

// ─── Timers ──────────────────────────────────────────────────────────────────

// tonTimer is a Timer On-Delay instruction.
type tonTimer struct {
	tag    string
	preset time.Duration
}

func (e *tonTimer) evaluate(rungIn bool, s *State) bool {
	t := s.getTimer(e.tag, e.preset)
	now := time.Now()

	if rungIn {
		t.EN = true
		if !t.DN {
			// Start or continue timing.
			if !t.TT {
				// First scan with rungIn true (or after reset).
				t.TT = true
				t.lastTick = now
			}
			// Accumulate elapsed time.
			t.ACC += now.Sub(t.lastTick)
			t.lastTick = now
			if t.ACC >= t.Preset {
				t.DN = true
				t.TT = false
			}
		} else {
			// Already done, keep lastTick current so we don't jump on reset.
			t.lastTick = now
		}
	} else {
		// Rung false: reset everything.
		t.ACC = 0
		t.DN = false
		t.EN = false
		t.TT = false
	}

	return t.DN
}

// TON creates a Timer On-Delay element. When the rung is continuously true
// for the preset duration, DN is set. DN resets when the rung goes false.
func TON(tag string, preset time.Duration) Element {
	return &tonTimer{tag: tag, preset: preset}
}

// tofTimer is a Timer Off-Delay instruction.
type tofTimer struct {
	tag    string
	preset time.Duration
}

func (e *tofTimer) evaluate(rungIn bool, s *State) bool {
	t := s.getTimer(e.tag, e.preset)
	now := time.Now()

	if rungIn {
		// Rung true: DN is true, reset accumulator, stop timing.
		t.DN = true
		t.EN = true
		t.TT = false
		t.ACC = 0
		t.lastTick = now
	} else {
		// Rung false.
		if t.prevEN && !rungIn {
			// Falling edge: start off-delay timing.
			t.TT = true
			t.lastTick = now
		}
		if t.TT {
			// Currently timing the off-delay.
			t.ACC += now.Sub(t.lastTick)
			t.lastTick = now
			if t.ACC >= t.Preset {
				t.DN = false
				t.TT = false
				t.EN = false
			}
		}
	}

	t.prevEN = rungIn
	return t.DN
}

// TOF creates a Timer Off-Delay element. DN is set immediately when the rung
// goes true, and remains set for the preset duration after the rung goes false.
func TOF(tag string, preset time.Duration) Element {
	return &tofTimer{tag: tag, preset: preset}
}

// ─── Counters ────────────────────────────────────────────────────────────────

// ctuCounter is a Count Up instruction.
type ctuCounter struct {
	tag    string
	preset int
}

func (e *ctuCounter) evaluate(rungIn bool, s *State) bool {
	c := s.getCounter(e.tag, e.preset)

	// Rising edge detection.
	if rungIn && !c.prevEN {
		c.ACC++
	}
	if c.ACC >= c.Preset {
		c.DN = true
	}
	c.prevEN = rungIn
	return c.DN
}

// CTU creates a Count Up element. Each rising edge of the rung increments the
// accumulator. DN is set when ACC reaches the preset.
func CTU(tag string, preset int) Element {
	return &ctuCounter{tag: tag, preset: preset}
}

// ctdCounter is a Count Down instruction.
type ctdCounter struct {
	tag    string
	preset int
}

func (e *ctdCounter) evaluate(rungIn bool, s *State) bool {
	c := s.getCounter(e.tag, e.preset)

	// Rising edge detection.
	if rungIn && !c.prevEN {
		c.ACC--
	}
	if c.ACC <= 0 {
		c.DN = true
	}
	c.prevEN = rungIn
	return c.DN
}

// CTD creates a Count Down element. Each rising edge of the rung decrements
// the accumulator. DN is set when ACC reaches zero or below.
func CTD(tag string, preset int) Element {
	return &ctdCounter{tag: tag, preset: preset}
}

// resElement resets a counter's accumulator and done bit.
type resElement struct{ tag string }

func (e *resElement) evaluate(rungIn bool, s *State) bool {
	if rungIn {
		if c, ok := s.counters[e.tag]; ok {
			c.ACC = 0
			c.DN = false
		}
		if t, ok := s.timers[e.tag]; ok {
			t.ACC = 0
			t.DN = false
			t.EN = false
			t.TT = false
		}
	}
	return rungIn
}

// RES creates a Reset element. When the rung is true, the referenced counter
// or timer accumulator and done bit are cleared.
func RES(tag string) Element { return &resElement{tag: tag} }

// ─── Structural ──────────────────────────────────────────────────────────────

// branchElement evaluates multiple parallel paths (OR logic).
type branchElement struct {
	paths [][]Element
}

func (e *branchElement) evaluate(rungIn bool, s *State) bool {
	result := false
	for _, path := range e.paths {
		pathResult := rungIn
		for _, elem := range path {
			pathResult = elem.evaluate(pathResult, s)
		}
		if pathResult {
			result = true
		}
	}
	return result
}

// Branch creates a parallel branch (OR). Each path is evaluated independently
// with the incoming rung state. The branch is true if ANY path is true.
func Branch(paths ...[]Element) Element {
	return &branchElement{paths: paths}
}

// seriesElement evaluates elements in sequence (AND logic).
type seriesElement struct {
	elements []Element
}

func (e *seriesElement) evaluate(rungIn bool, s *State) bool {
	result := rungIn
	for _, elem := range e.elements {
		result = elem.evaluate(result, s)
	}
	return result
}

// Series creates a series chain of elements. Each element receives the result
// of the previous element.
func Series(elements ...Element) Element {
	return &seriesElement{elements: elements}
}

// ─── Program Creation ────────────────────────────────────────────────────────

// CreateProgram builds a plc.ProgramFunc from a ladder logic definition.
// The ladder state persists across scans.
//
// Example:
//
//	program := ladder.CreateProgram(func(rung func(elements ...ladder.Element)) {
//	    rung(ladder.NO("startButton"), ladder.OTE("motorRunning"))
//	    rung(ladder.NO("stopButton"), ladder.OTU("motorRunning"))
//	})
func CreateProgram(build func(rung func(elements ...Element))) plc.ProgramFunc {
	var rungs [][]Element
	build(func(elements ...Element) {
		rungs = append(rungs, elements)
	})

	state := NewState()
	return func(vars *plc.Variables, update plc.UpdateFunc) {
		// Sync PLC variables into ladder tags.
		for id, v := range vars.All() {
			state.setTag(id, v.BoolValue())
		}
		// Evaluate all rungs.
		for _, elements := range rungs {
			rungResult := true // rung starts energized
			for _, elem := range elements {
				rungResult = elem.evaluate(rungResult, state)
			}
		}
		// Sync ladder tags back to PLC variables.
		for id := range vars.All() {
			if val, ok := state.tags[id]; ok {
				update(id, val)
			}
		}
	}
}
