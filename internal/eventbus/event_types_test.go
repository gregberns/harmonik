package eventbus_test

import "github.com/gregberns/harmonik/internal/core"

func init() {
	for _, eventType := range []core.EventType{
		busImplFixtureEventType,
		cascadeParentType,
		cascadeChildType,
		h7EventType,
		hc034FixtureEventType,
	} {
		if err := core.RegisterEventType(eventType, func() core.EventPayload { return &struct{}{} }); err != nil {
			panic("eventbus test event registration: " + err.Error())
		}
	}
}
