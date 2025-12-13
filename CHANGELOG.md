# CHANGELOG

## next

- feature: add `UpdateID` to `Depth` and track order book state changes
- refactor: split `handleOrder` into separate handlers for each order type
- refactor: replace `Trade` with `BookLog` to capture Open, Match, and Cancel events for full order book reconstruction


## v0.6.0 (2023-10-20)

- feature: PublishTrader
- refactor: test case can run parallel

## v0.5.1 (2023-09-02)

- refactor: remove all mutex loc

## v0.5.0 (2023-07-02)

- feature: get orderbook's depth
- refactor: change orderbook to async
- refactor: rename `OrderType`, `Side` constants
- refactor: rename `PlaceOrder` to `AddOrder` in orderbook
- refactor: allow `tradeChan` pass from outside when engine is created
- refactor: benchmark test

## v0.4.1 (2023-04-09)

- feature: New matching engine which manages multiple order books.
- feature: add `CancelOrder` function to orderbook

## v0.4 (2023-04-08)

- refactor: add OrderType field (`market`, `limit`, `ioc`, `post_only`, `fok`)
- refactor: add more testcase
- fix: fix market order size issue

## v0.3 (2022-06-05)

- add order book update event
- add time in force strategy (gtc, ioc)
- add "post only" behavior

## v0.2 (2022-05-11)

- use skiplist algorithm for order queue
- redesign matching engine arch
- add benchmark test

## v0.1

- just for fun
