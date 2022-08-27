# Geocoding with Redis

A geocoder is a service for matching addresses to geographic locations and the entities containing those addresses. Geocoders use both geospatial queries and fuzzy text search to resolve a partial address to an address and location (coordinates) from a validated set of addresses.

For example, if a user wants to resolve the address `TIMES SQ MANHATTAN`, a geocoder may use a full text search algorithm to propose the following addresses:

- 5 TIMES SQUARE MANHATTAN 10036  ( -73.98723, 40.755985 )
- 10 TIMES SQUARE MANHATTAN 10018 ( -73.98687, 40.754963 )
- 11 TIMES SQUARE MANHATTAN 10036 ( -73.98984, 40.756763 )

[Insert app screenshots](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#uploading-assets)

# Overview video (Optional)

Here's a short video that explains the project and how it uses Redis:

[Insert your own video here, and remove the one below]

[![Embed your YouTube video](https://i.ytimg.com/vi/vyxdC1qK4NE/maxresdefault.jpg)](https://www.youtube.com/watch?v=vyxdC1qK4NE)


## How it works

Please refer to service and database names in the [architecture pdf](https://github.com/DMW2151/gcaas/blob/main/_arch.pdf) in the following sub-sections.

### How the data is stored:

* Syncronous Geocoder API

	* `Redis Search` - Stores validated addresses for our Geocoder, e.g. the superset of all possible results.

		* Address - A [HASH](https://redis.io/docs/data-types/hashes/) identified by `addressId` (e.g. `60f011eb-3817-4b67-abed-af4a9aa50623`), containing fields for `composite_street_address` (e.g. `23 Wall Street, New York, NY`) and `location` (e.g. `(-74.008827, 40.706005)`)

	* `Geocoder Web Cache` - Temporarily stores the responses of recent Geocoder API calls. Prevents duplicate requests from hitting `Redis Search` in a short window.

		* Request Key - A deterministically-generated request key is stored as a [STRING](https://redis.io/docs/data-types/strings/). The value of the key is the string representation of a set of 


### How the data is accessed:

Refer to [this example](https://github.com/redis-developer/basic-analytics-dashboard-redis-bitmaps-nodejs#how-the-data-is-accessed) for a more detailed example of what you need for this section.


### Performance Benchmarks

[If you migrated an existing app to use Redis, please put performance benchmarks here to show the performance improvements.]


## How to run it locally?

[Make sure you test this with a fresh clone of your repo, these instructions will be used to judge your app.]


### Prerequisites

- docker -> `Docker version 20.10.14, build a224086`
- docker-compose -> `docker-compose version 1.29.2`
- go -> `go version go1.18.5 darwin/amd64`


### Local installation

[Insert instructions for local installation]

## Deployment

This application does not provide a quick deploy option, pleas refer to the Local Installation section for discussion on how to test the app








## Usage

### API

The public API for this service exposes the following endpoints

- https://gc.dmw2151.com/geocode/
- https://gc.dmw2151.com/batch/
- https://gc.dmw2151.com/batch/${BATCH_ID}/status

The private API for this service allows the following additional GRPC calls

- `/geocoder.Geocoder/InsertorReplaceAddressData`


### Web

## Development Notes

### Architecture

### Initial Deployment

- insert from local 

Use script (`./srv/cmd/ingest/main.go`) that streams [data](https://data.cityofnewyork.us/City-Government/NYC-Address-Points/g6pj-hd8k) into our backend redis via a call to `InsertorReplaceAddressData` at `_gcaas._tcp.dmw2151.com`. 

verify backend 

- curl -XPOST https://gc.dmw2151.com/locations/ -d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "MACON"}' 


curl -XGET localhost:2151/batch/9d75025a-21c4-4d20-95ac-2e4a8aafc638
curl -XPOST localhost:2151/batch/ -d @/Users/dustinwilson/Desktop/Personal/misc-hackathon-projects/dumb-tests/small-test.json

```bash
wget "https://data.cityofnewyork.us/api/views/emzr-v3pi/rows.csv?accessType=DOWNLOAD" -O ./_data/nyc_addr.csv
```

```bash
wc -lc  ./_data/nyc_addr.csv 

967508 137496309 ./_data/nyc_addr.csv
```

```csv
the_geom,ADDRESS_ID,BIN,H_NO,HNO_SUFFIX,HYPHEN_TYP,SIDE_OF_ST,SPECIAL_CO,BOROCODE,ZIPCODE,CREATED,MODIFIED,ST_NAME,HN_RNG,HN_RNG_SUF,PHYSICALID,PRE_MODIFI,PRE_DIRECT,PRE_TYPE,POST_TYPE,POST_DIREC,POST_MODIF,FULL_STREE
POINT (-73.94890840262882 40.681024605257534),3066687,3053232,54,,N,2,,3,11216,02/13/2009 12:00:00 AM,01/18/2013 12:00:00 AM,MACON,,,66261,,,,ST,,,MACON ST
POINT (-73.94867469809614 40.6862985441319),3064205,3051113,438,,N,2,,3,11216,02/13/2009 12:00:00 AM,,GATES,,,68956,,,,AVE,,,GATES AVE
POINT (-73.95302508854085 40.6880523616944),3063204,3050344,442,,N,2,,3,11216,02/13/2009 12:00:00 AM,,GREENE,,,44078,,,,AVE,,,GREENE AVE
```


ENVIRONMENT=DEV DO_SPACES_SECRET=${DO_SPACES_SECRET} DO_SPACES_KEY=${DO_SPACES_KEY} docker compose up


./srv/cmd/ingest/main.go
INFO[0026] /geocoder.Geocoder/InsertorReplaceAddressData; success  insert.num_objects=967507 insert.success=true


