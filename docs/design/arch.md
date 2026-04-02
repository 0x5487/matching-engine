# Architecture Rules

## 1. Event Time and Determinism

- Every command entering the matching engine must carry an upstream-assigned logical `Timestamp`.
- Every command entering the matching engine must carry an upstream-assigned non-empty `CommandID`.
- Upstream means the Sequencer, Gateway, OMS, or any producer responsible for creating the canonical event.
- The matching engine must use that `Timestamp` for business logs, replay, and any deterministic state transition semantics.
- The matching engine must not synthesize business timestamps with local `time.Now()` for commands that participate in replay.
- Missing or non-positive `Timestamp` is an invalid payload and must be rejected on the canonical event path.
- Missing `CommandID` is an invalid payload and must be rejected on the canonical event path.

## 2. Log Model Boundaries

- `OrderBookLog` is part of the deterministic event model and should contain only replay-stable fields.
- Non-deterministic local observation fields such as `CreatedAt`, `ReceivedAt`, or `PublishedAt` must not live inside `OrderBookLog`.
- If downstream systems need local processing time, they should attach it in the `PublishLog` implementation or in an external envelope model.
- Actor identity that must survive audit or replay analysis belongs in the canonical event model, not only in downstream transport metadata.

## 3. PublishLog Responsibilities

- `PublishLog` receives canonical engine events.
- Successful management commands are part of the canonical event stream and must be emitted through `PublishLog`.
- `PublishLog` implementations may enrich events with local ingest time, persistence time, or network publish time for observability.
- Such enrichment must be treated as downstream metadata and must not be fed back into replay or state-rebuild logic.

## 4. Replay Contract

- Replaying the same command stream must produce identical deterministic fields in emitted logs and identical order book state.
- The canonical event time for replay comparison is `Timestamp`, not any downstream-added local clock field.
