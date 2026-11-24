// Package market holds the product's types and, later, every rule about time.
package market

import "time"

// Market says where an instrument trades. "B3" and "US" are the two stock
// markets; "IDX" holds the indices of the summary line and "FX" the exchange
// rate.
type Market string

const (
	B3  Market = "B3"
	US  Market = "US"
	IDX Market = "IDX"
	FX  Market = "FX"
)

// Instrument is a ticker with its market, free of any source's notation.
// Watchlist tickers: {B3 PETR4}, {US AAPL}. Summary entries: {IDX IBOV},
// {IDX SP500}, {FX USDBRL}.
type Instrument struct {
	Market Market
	Symbol string
}

// Point is one price of the intraday series.
type Point struct {
	T time.Time
	V float64
}

// Session is the regular trading period the source reports with a quote.
type Session struct {
	Start, End time.Time
	TZ         string // IANA name, such as "America/Sao_Paulo"
}

// Quote is what a source returns for one instrument.
type Quote struct {
	Name, Currency              string // Currency: "BRL", "USD", or "" for an index
	Price, PrevClose, Low, High float64
	Time                        time.Time // of the last price
	Series                      []Point   // regular session of one trading day, oldest first
	Session                     Session
}
