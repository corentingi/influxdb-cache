# InfluxDB Cache

This package is a simple reverse-proxy* that split an InfluxDB request into smaller cacheable pieces.

This package is still in development and is not yet suitable to production workloads.

Also You might want to use a *reverse proxy* such as Nginx in front of this to filter all queries that are not /queries (And all post queries) as those are not supported yet.


## Why not use Continuous Queries ?

Continuous queries are fine. But they are not flexible at all...
Also they do not solve as much problems as caching does.


## How does it work ?

When making a request to the cache, the request is being parsed and split in several pieces:

```SELECT mean(cpu) FROM cpu WHERE time > now() - 6h GROUP BY time(10s)```

This request can be split across it's time range in smaller pieces.
This allows us to cache those small pieces and re-use them in a following request.

Each cacheable piece only depends on the defined aggregation time.
This means that every time the request is sent, most of the underlying pieces will match existing ones.
Of course the first and last pieces will often be uncacheable.

// svg
// Showing a request, it's time range and where the request is beeing separated.


## Ideal infrastructure

Keep in mind that using this package adds at least 2 layers between your client and InfluxDB.

I would also suggest to have all 3 proxy layers on the same machine.

```
NGINX -> Cacher -> Varnish -> InfluxDB
```

See **examples** directory for VCL example file.


## Configuration


## TODO

- If none of statements in the query is cacheable don't Unmarshall anything
- Handle no result influxdb errors
- Check Time zone issue (if it really is there)
- Request parallelization
- Better logging system
- Handle errors and status codes
- Proxy headers in both ways ?
- Use initial time boundaries (now() and stuff) instead of always replacing it (Open to discussion)
- Rethink the Chunksize (Probably using a non-linear function of group-by-interval)
