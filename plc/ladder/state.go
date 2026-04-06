package ladder

import (
	"strings"
	"time"
)

// State holds all ladder runtime state (tag values, timers, counters).
type State struct {
	tags     map[string]bool
	timers   map[string]*timerState
	counters map[string]*counterState
}

// NewState creates a new empty ladder state.
func NewState() *State {
	return &State{
		tags:     make(map[string]bool),
		timers:   make(map[string]*timerState),
		counters: make(map[string]*counterState),
	}
}

// getTag returns the boolean value of a tag. It also resolves timer and
// counter sub-tags such as "myTimer.DN", "myTimer.EN", "myTimer.TT",
// and "myCounter.DN".
func (s *State) getTag(tag string) bool {
	// Check plain tags first.
	if v, ok := s.tags[tag]; ok {
		return v
	}
	// Check timer/counter sub-tags (e.g., "myTimer.DN").
	parts := strings.SplitN(tag, ".", 2)
	if len(parts) == 2 {
		if t, ok := s.timers[parts[0]]; ok {
			switch parts[1] {
			case "DN":
				return t.DN
			case "EN":
				return t.EN
			case "TT":
				return t.TT
			}
		}
		if c, ok := s.counters[parts[0]]; ok {
			switch parts[1] {
			case "DN":
				return c.DN
			}
		}
	}
	return false
}

func (s *State) setTag(tag string, v bool) { s.tags[tag] = v }

// timerState holds the runtime state for a single timer instruction.
type timerState struct {
	Preset   time.Duration
	ACC      time.Duration // accumulated time
	DN       bool          // done
	EN       bool          // enabled
	TT       bool          // timing (currently counting)
	lastTick time.Time     // last scan time when enabled
	prevEN   bool          // previous enable state (for edge detection in TOF)
}

// getTimer returns the timer state for the given tag, creating it if it does
// not yet exist. The preset is only applied when the timer is first created.
func (s *State) getTimer(tag string, preset time.Duration) *timerState {
	if t, ok := s.timers[tag]; ok {
		return t
	}
	t := &timerState{Preset: preset}
	s.timers[tag] = t
	return t
}

// counterState holds the runtime state for a single counter instruction.
type counterState struct {
	Preset int
	ACC    int
	DN     bool
	prevEN bool // for rising edge detection
}

// getCounter returns the counter state for the given tag, creating it if it
// does not yet exist. The preset is only applied when the counter is first
// created.
func (s *State) getCounter(tag string, preset int) *counterState {
	if c, ok := s.counters[tag]; ok {
		return c
	}
	c := &counterState{Preset: preset}
	s.counters[tag] = c
	return c
}
