# SuperNovaCore 

SupernovaCore is a Cosmos SDK v2 compatible consensus engine designed to be a drop in replacement for CometBFT v1. 
The following features are implemented in this version of SupernovaCore

1. August, 2024 version of HotStuff 1 latency consensus alogrithm (the original HotStuff-2 consensus
has been running on Meter mainnet with more than 300 physical committee validator ndoes for more than 4 years).

2. BLS signature aggregations for validator votes

3. Dedicated validator messaging subnet to improve the communication efficiency



## Build & Run

```
go build -tags bls12381 main.go sample_app.go
./main start 
```
