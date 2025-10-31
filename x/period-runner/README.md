# Period Runner

The period runner emits time-based SBCP period events derived from a genesis time and an Ethereum-epoch cadence.
Components use it to receive a callback at the start of every period.

## Architecture

```mermaid
classDiagram
    class PeriodRunner {
        <<interface>>
        +SetHandler(PeriodCallback)
        +Start(ctx context.Context) error
        +Stop(ctx context.Context) error
        +PeriodForTime(t time.Time) (uint64, time.Time)
    }

    class PeriodRunnerConfig {
        +Handler PeriodCallback
        +EpochsPerPeriod uint64
        +GenesisTime time.Time
        +Now func() time.Time
        +Logger zerolog.Logger
    }

    class LocalPeriodRunner {
        -handler PeriodCallback
        -periodDuration time.Duration
        -now func() time.Time
        -genesisTime time.Time
        -log zerolog.Logger
        -cancel context.CancelFunc
        -started bool
        +SetHandler(PeriodCallback)
        +Start(ctx) error
        +Stop(ctx) error
        +PeriodForTime(t) (uint64, time.Time)
    }

    PeriodRunner <|.. LocalPeriodRunner
    LocalPeriodRunner --> PeriodRunnerConfig : constructed with
```

`LocalPeriodRunner` computes a fixed `periodDuration = EpochsPerPeriod * EthSlotsPerEpoch * EthSlotDuration` and emits an event at `genesis + K * periodDuration` for `K = 0, 1, 2, ...`. On start, it immediately emits the current period if the current time is on or after genesis, then schedules a timer for the next period. If multiple periods were missed, it will emit each missed period in order before scheduling the next one.

## Flow

```mermaid
sequenceDiagram
    participant App
    participant Runner as LocalPeriodRunner
    participant Timer

    App->>Runner: SetHandler(cb)
    App->>Runner: Start(ctx)
    alt now < genesis
        Runner->>Timer: Arm(genesis - now)
    else now >= genesis
        Runner->>Runner: PeriodForTime(now) -> (id, start)
        Runner->>App: cb(PeriodInfo{id, start, duration})
        Runner->>Timer: Arm(nextStart - now)
    end
    loop On timer
        Timer-->>Runner: tick
        alt missed periods
            Runner->>Runner: PeriodForTime(now) -> (currentID)
            Runner->>App: cb for each id in [last+1..currentID]
        end
        Runner->>Timer: Arm(nextStart - now)
    end
```

## Tests

Run the unit tests with:
```bash
go test -v ./...
```

**Tips for adding more tests**
- Override `periodDuration` directly on the concrete `*LocalPeriodRunner` in tests to speed up execution.
- Provide a custom `Now` function guarded by a mutex to advance time deterministically.
- Use a buffered channel in the handler to collect `PeriodInfo` and assert sequence and timestamps.

## Error Handling

Handler errors are logged and stop the emission loop (the goroutine returns). Callers should restart the runner if needed.

