# SCP Instance Supervisor

Supervises the lifecycle of multiple SCP publisher instances: creation, vote handling, timeout management, and finalization.
It exposes a finalize hook for when an instance terminates to trigger follow-up actions (e.g., notify the SBCP) and maintains a bounded/retained history of decisions.


Key responsibilities:
- Create SCP instances via `SCPFactory`, run them, and track them by InstanceID.
- Route votes to instances and finalize when they reach Accepted/Rejected.
- Fire a timer per instance to enforce timeouts; finalize on expiry.
- Use the network to broadcast Decided messages (internal to the SCP instance).
- On finalization, invoke `OnFinalize` if provided.

## Architecture

```mermaid
classDiagram
    class Config {
        +Factory SCPFactory
        +Network scp.PublisherNetwork
        +TimerFactory TimerFactory
        +InstanceTimeout time.Duration
        +Now func() time.Time
        +MaxHistory int
        +HistoryRetention time.Duration
        +OnFinalize OnFinalizeHook
    }

    class Supervisor {
        +StartInstance(ctx, queued, instance) error
        +HandleVote(ctx, vote) error
        +History() []CompletedInstance
        +SetOnFinalizeHook(fn)
        -handleTimeout(instance)
        -tryFinalize(ctx, entry, source)
    }

    class SCPFactory {
        <<function>>
        +(instance, network, logger) -> scp.PublisherInstance
    }

    Supervisor <-- Config : constructed with
    Config --> SCPFactory : includes
    Supervisor --> SCPFactory : uses to create instances
    Supervisor --> TimerFactory : set timer per instance
```

## Sequence Diagram

```mermaid
sequenceDiagram
    participant SBCP
    participant Scheduler
    participant Supervisor
    participant Instance
    participant Timer

    SBCP->>Scheduler: StartInstance granted
    Scheduler->>Supervisor: StartInstance(queued, instance)
    Supervisor->>Instance: Factory(instance, network, logger)
    Supervisor->>Timer: AfterFunc(instanceTimeout, timeout)
    Supervisor->>Instance: Run()

    loop Votes
        participant Peer as Peer
        Peer->>Supervisor: HandleVote(vote)
        Supervisor->>Instance: ProcessVote(chain, vote)
        alt Decision reached
            Instance-->>Supervisor: DecisionState() != Pending
            Supervisor->>Supervisor: tryFinalize(source=message)
        end
    end

    alt Timeout fires
        Timer-->>Supervisor: timeout callback
        Supervisor->>Instance: Timeout()
        Instance-->>Supervisor: DecisionState() != Pending
        Supervisor->>Supervisor: tryFinalize(source=timeout)
    end

    Supervisor->>History: append(decision)
    Supervisor->>Scheduler: OnFinalize hook (if set)
    Scheduler->>SBCP: instance decided
```
