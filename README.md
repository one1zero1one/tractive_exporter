# Tractive Prometheus Exporter

`Prometheus.io` exporter for Tractive's `my.tractive.com` public sharing location of your lovely pets. 

Loosely based on [this article](https://medium.com/teamzerolabs/15-steps-to-write-an-application-prometheus-exporter-in-go-9746b4520e26).

## How To

### Activate Public Sharing for Each Pet (Tracker)

From `https://my.tractive.com/#/settings/pets/`, for each pet, go to `Share`. Activate the `public URL`. Pick up the ID (it's the 10 digits code after `../public_share/` in the `public URL`).

### Run the Exporter With a List of Trackers

The exporter takes a comma delimited list of IDs as:

- environment variable, e.g. `TRACTIVE_PUBLIC_SHARES=1234567890,1234567891` (can also be passed as a `.env` file)
- parameter, e.g `-trackers.list=6a7235da65,2d1b273ec8`

Run 
```
make run
```

### Scrape with Prometheus

```
# config
```

### Metrics


### Grafana Dashboard


### PromQL Alerts

Doc
## What it does

### Info

TODO: https://graph.tractive.com/3/public_share/6a7235da65/info


### Position

https://graph.tractive.com/3/public_share/6a7235da65/position

