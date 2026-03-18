## ADDED Requirements

### Requirement: Event Type Definition

The event bus package SHALL define an `EventType` enumeration with the following values: `EventCardMoved`, `EventCardUpdated`, `EventCardsRefreshed`, `EventSyncStarted`, `EventSyncCompleted`, `EventSyncFailed`. Future event types (`EventAgentStarted`, `EventAgentOutput`, `EventAgentCompleted`) SHALL be defined but not used in this phase. An `Event` struct SHALL contain `Type` (EventType), `Payload` (interface{}), and `At` (time.Time).

#### Scenario: Event struct contains required fields
- **WHEN** an event is created with a type, payload, and timestamp
- **THEN** all three fields SHALL be accessible on the event struct

### Requirement: EventBus Interface

The event bus SHALL implement the `EventBus` interface as defined in spec.md section 3.4:
- `Publish(event Event)` -- sends an event to all subscribers of that event type
- `Subscribe(eventType EventType) <-chan Event` -- returns a channel that receives events of the given type
- `Unsubscribe(ch <-chan Event)` -- removes the subscription and closes the channel

#### Scenario: Interface contract
- **WHEN** a new event bus is created
- **THEN** it SHALL implement all three methods of the EventBus interface

### Requirement: Publish Fans Out to Subscribers

When `Publish` is called with an event, the event SHALL be sent to all channels that were subscribed to that event's type. Subscribers to other event types SHALL NOT receive the event. Publishing SHALL be non-blocking for the caller.

#### Scenario: Single subscriber receives event
- **WHEN** a subscriber is subscribed to `EventCardMoved` and an `EventCardMoved` event is published
- **THEN** the subscriber's channel SHALL receive the event

#### Scenario: Multiple subscribers receive same event
- **WHEN** three subscribers are subscribed to `EventSyncCompleted` and an `EventSyncCompleted` event is published
- **THEN** all three subscriber channels SHALL receive the event

#### Scenario: Subscriber does not receive unrelated events
- **WHEN** a subscriber is subscribed to `EventCardMoved` and an `EventSyncStarted` event is published
- **THEN** the subscriber's channel SHALL NOT receive the event

### Requirement: Subscribe Returns Buffered Channel

The `Subscribe` method SHALL return a buffered channel with a buffer size of 64. This prevents slow subscribers from blocking the publisher. A single subscriber MAY subscribe to multiple event types by calling `Subscribe` multiple times with different event types.

#### Scenario: Buffered channel allows non-blocking publish
- **WHEN** a subscriber is subscribed but not reading from the channel, and fewer than 64 events are published
- **THEN** all published events SHALL be buffered in the channel without blocking the publisher

#### Scenario: Multiple subscriptions from one consumer
- **WHEN** a consumer calls `Subscribe` for both `EventCardMoved` and `EventCardUpdated`
- **THEN** the consumer SHALL receive events of both types on their respective channels

### Requirement: Publish Drops Events on Full Buffer

When a subscriber's channel buffer is full (64 events buffered without being consumed), the `Publish` method SHALL drop the event for that subscriber rather than blocking. The dropped event SHOULD be logged. Other subscribers with available buffer space SHALL still receive the event.

#### Scenario: Full buffer causes drop, not block
- **WHEN** a subscriber's channel has 64 unread events and another event is published
- **THEN** the new event SHALL be dropped for that subscriber, the publish call SHALL NOT block, and other subscribers SHALL still receive the event

### Requirement: Unsubscribe Removes and Closes Channel

The `Unsubscribe` method SHALL remove the given channel from the subscriber map and close the channel. After unsubscribing, the channel SHALL NOT receive any further events. Calling `Unsubscribe` with a channel that is not subscribed SHALL be a no-op (no panic, no error).

#### Scenario: Unsubscribed channel receives no more events
- **WHEN** a subscriber unsubscribes and then an event of the previously subscribed type is published
- **THEN** the unsubscribed channel SHALL NOT receive the event and reading from it SHALL indicate the channel is closed

#### Scenario: Unsubscribe with unknown channel
- **WHEN** `Unsubscribe` is called with a channel that was never returned by `Subscribe`
- **THEN** the call SHALL complete without error or panic

### Requirement: Thread Safety

The event bus SHALL be safe for concurrent use. Multiple goroutines MAY call `Publish`, `Subscribe`, and `Unsubscribe` concurrently without data races. The implementation SHALL use `sync.RWMutex` to protect the subscriber map.

#### Scenario: Concurrent publish and subscribe
- **WHEN** multiple goroutines publish events and subscribe/unsubscribe concurrently
- **THEN** no data races SHALL occur and all operations SHALL complete without panics

### Requirement: Constructor

The event bus package SHALL provide a `New` function that returns an initialized `EventBus` implementation ready for use. No configuration parameters are required.

#### Scenario: New bus is immediately usable
- **WHEN** `New()` is called
- **THEN** the returned bus SHALL accept `Publish`, `Subscribe`, and `Unsubscribe` calls immediately
