# SBCP Controller

Wraps an `sbcp.Publisher` with queue management, period handling, settled-state updates, and proof-timeout recovery.
SCP instance creation remains delegated to an external `InstanceStarter` (e.g. the SCP Instance Supervisor).

## Architecture

```mermaid
classDiagram
    class Config {
        +Publisher sbcp.Publisher
        +Queue queue.XTRequestQueue
        +InstanceStarter InstanceStarter
        +Now func:time.Time
        +Logger zerolog.Logger
    }

    class Controller {
        <<interface>>
        +EnqueueXTRequest(ctx, req, from) error
        +TryProcessQueue(ctx) error
        +OnNewPeriod(ctx) error
        +NotifyInstanceDecided(ctx, instance) error
        +AdvanceSettledState(number, hash) error
        +ProofTimeout(ctx)
    }

    class InstanceStarter {
        <<function>>
        +StartInstance(ctx, queued, instance) error
    }

    Controller <|.. controllerImpl
    controllerImpl <-- Config : constructed with
    controllerImpl --> InstanceStarter
    controllerImpl --> sbcpPublisher
    controllerImpl --> queueXTRequestQueue
```

## Sequence

```mermaid
sequenceDiagram
    participant Runner as PeriodRunner
    participant Ctrl as Controller
    participant Queue
    participant Publisher as sbcp.Publisher
    participant Starter as InstanceStarter

    Runner->>Ctrl: OnNewPeriod()
    Ctrl->>Publisher: StartPeriod()
    Ctrl->>Ctrl: TryProcessQueue()
    loop Drain queue
        Ctrl->>Queue: Peek()
        alt queue empty
            Ctrl-->>Ctrl: stop
        else request available
            Ctrl->>Publisher: StartInstance(composeReq)
            alt ErrCannotStartInstance
                Publisher-->>Ctrl: sbcp.ErrCannotStartInstance
                Ctrl-->>Ctrl: return (chains busy)
            else started
                Ctrl->>Queue: Dequeue()
                Ctrl->>Starter: StartInstance(queued, instance)
                alt ErrInstanceAlreadyActive
                    Starter-->>Ctrl: error
                    Ctrl->>Queue: Requeue(queued)
                    Ctrl-->>Ctrl: return
                else success
                    Starter-->>Ctrl: ok
                    Ctrl-->>Ctrl: continue loop
                end
            end
        end
    end

    Note over Ctrl: NotifyInstanceDecided()
    Ctrl->>Publisher: DecideInstance(instance)
    Ctrl->>Ctrl: TryProcessQueue()

    Note over Ctrl: ProofTimeout()
    Ctrl->>Publisher: ProofTimeout()
    Ctrl->>Ctrl: TryProcessQueue()
```
