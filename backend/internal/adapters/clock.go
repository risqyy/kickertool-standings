package adapters

import (
	"time"

	"kickertool-ranking/internal/ports"
)

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
func (SystemClock) NewTicker(interval time.Duration) ports.Ticker {
	return systemTicker{ticker: time.NewTicker(interval)}
}

type systemTicker struct{ ticker *time.Ticker }

func (t systemTicker) Chan() <-chan time.Time { return t.ticker.C }
func (t systemTicker) Stop()                  { t.ticker.Stop() }
