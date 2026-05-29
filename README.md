# Mean Reversion Candidate

Go web app for ranking every symbol in `top-100.csv` as an intraday mean-reversion candidate using Massive minute candles.

## Run

1. Put your Massive key in `.env`:

```sh
MASSIVE_API_KEY=your_key
```

2. Start the app:

```sh
go run .
```

3. Open:

```text
http://localhost:8089
```

The port, CSV path, live window, replay window, scoring thresholds, and chart opener base URL are configurable in `config.yaml`. Stock links default to the separate chart opener at `http://localhost:8081`.

## Scoring

Every CSV symbol is always ranked. The score is:

```text
VWAP extension (25)
+ 30m statistical move (20)
+ daily ATR move (20)
+ 60m range extension (12)
+ liquidity (13)
+ reversal evidence (10)
- trend persistence penalty (15)
```

Higher scores mean a cleaner mean-reversion setup. `Long bounce` means price is stretched below fair value; `Short fade` means price is stretched above fair value. ATR columns show the prior 14-day average true range, today’s move in ATRs, and the VWAP stretch in ATRs.

## Historical Mode

Use the date picker and minute slider to jump to any minute from 04:00 to 20:00 New York time. Press `Play` to replay the day one minute per second.
