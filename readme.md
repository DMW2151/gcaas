# Geocoding with Redis

A geocoder is a service for matching addresses to geographic locations and the entities containing those addresses. Geocoders use both geospatial queries and fuzzy text search to resolve a partial address to an address and location (coordinates) from a validated set of addresses.

For example, if a user wants to resolve the address `TIMES SQ MANHATTAN`, a geocoder may use a full text search algorithm to propose the following addresses:

- 5 TIMES SQUARE MANHATTAN 10036  ( -73.98723, 40.755985 )
- 10 TIMES SQUARE MANHATTAN 10018 ( -73.98687, 40.754963 )
- 11 TIMES SQUARE MANHATTAN 10036 ( -73.98984, 40.756763 )

This application uses Redis Search and PubSub to provide a syncronous and asyncrounous geocoding service.

## Introduction Video


## Usage

Please refer to service and database names in the [architecture pdf](https://github.com/DMW2151/gcaas/blob/main/_arch.pdf) while reading the following sub-sections. In the following examples I default to the commands relevant for **forward** geocoding unless specified. This application exposes syncronous and asyncronous functionality.

* The syncronous geocoding API is diagramed on page one of the PDF above. The API allows a user to submit 1 query address (identified by either location or address string) and recieve a list of scored, proposed, best-match addresses.
	
```bash
# Request
curl -XPOST https://gc.dmw2151.com/geocode/ \
	-d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "ATLANTIC AVE BROOKLYN"}' 

# Response
{
  "result": [
    {
      "address": {
        "location": {
          "latitude": -73.909355,
          "longitude": 40.676468
        },
        "id": "address:50d17807-7dbd-41da-86c5-20c24f78ab36"
      },
      "normed_confidence": 1,
      "full_street_address": "2111 ATLANTIC AVE BROOKLYN 11233"
    }
  ],
  "num_results": 1
}
```

* The asyncronous geocoding API is diagramed on page two of the PDF above. The API allows a user to submit a batch of addresses to the API and download a file with the best match from the validation set for each address in the query. The asyncronous geocoding API has two endpoints.

	* `/batch/` - Allows for the submission of a new batch of data.
```bash
# Request - Creates a New Batch w. Three Addresses
curl -XPOST https://gc.dmw2151.com/batch/ \
	-d '{ 
		"method": "FWD_FUZZY", 
        "query_addr": [
                "ATLANTIC AVE BROOKLYN",
                "WALL STREET MANHATTAN",
                "509 MAIN ST",
        ]
	}' 

# Response - Acknowledges Batch Creation and Gives UUID for Status Updates
{
	"id": "60f011eb-3817-4b67-abed-af4a9aa50623",
	"status": 1,
	"update_time": {
		"seconds": 1661575183,
		"nanos": 391420781
	}
}
```

	* `/batch/${BATCH_UUID}` - returns the status of the batch, if the batch is completed, the body will include a signed URL that can be used to download or share the results for several days.
```bash
# Request - Get Status
curl -XGET https://gc.dmw2151.com/batch/60f011eb-3817-4b67-abed-af4a9aa50623

# Response - Get Status -> OK 
{
	"id": "60f011eb-3817-4b67-abed-af4a9aa50623",
	"status": 5, 
	"update_time": {
		"seconds": 1661575185,
		"nanos": 391420781
	}
	"download_path": "https://gcaas-data-storage.nyc3.digitaloceanspaces.com/datasets/...."
}
```

-------------

### How Data is Stored and Accessed

#### Syncronous Geocode Requests

* `Redis Search` - Stores validated addresses for our Geocoder, e.g. the superset of all possible results.

	* **Address** - A [HASH](https://redis.io/docs/data-types/hashes/) identified by `addressId` (e.g. `address:c24cf11b-79fb-4f78-a76b-7532c58a85ca`), containing fields for `composite_street_address` (e.g. `23 Wall Street, New York, NY 10005`) and `location` (e.g. `(-74.008827, 40.706005)`). 

		* Each address is stored during the initial data ingestion stage (see: `Management Service`) with a command similar to the following.

		```bash
		HSET address:${ADDRESS_UUID} location "(-74.008827, 40.706005)" composite_street_addr "23 Wall Street, New York, NY 10005"
		```

	* **Index** - An [Index](https://redis.io/docs/stack/search/quick_start/#create-an-index) of all `address:*` hashes. 

		* The index is created on server initialization with the following command.

		```bash
		FT.CREATE addr-idx ON HASH PREFIX 1 "address:" NOHL NOOFFSETS LANGUAGE "english" SCHEMA location GEO composite_street_address TEXT SORTABLE
		```

		* The `Geocoder GRPC Service` accesseses the index (and addresses) on each API call. When `Geocoder GRPC Service` recieves a request from `Geocoder Edge`, the HTTP request parameters populate a search similar to the following.

		```bash
		# Forward Geocode Request :: Address -> Fuzzy Match -> (Address, Location)
		FT.SEARCH addr-idx "@composite_street_address:${REQUEST_ADDR}" WITHSCORES LANGUAGE "english" SCORER TFIDF.DOCNORM LIMIT 0 ${REQUEST_MAX_RESULTS}

		# Reverse Geocode Request :: Point -> Geo Query -> (Address, Location)
		FT.SEARCH addr-idx "@location:[${REQUEST_LNG} ${REQUEST_LAT} 1024 m]" WITHSCORES LIMIT 0 ${REQUEST_MAX_RESULTS}
		```


* `Geocoder Web Cache` - Temporarily stores the responses of recent Geocoder API calls. Prevents duplicate requests from hitting `Redis Search` in a short window.

	* **Request Key** - A deterministically-generated request key is stored as a [STRING](https://redis.io/docs/data-types/strings/). In practice, the key is simply a concatenation of the request parameters (e.g. `FWD_GEOCODE:WALL_STREET_NY:5`). The value of the key is the string representation of the query response. 

		* The request key is set following each successful API call to `Geocoder Edge` with a command similar to the following.

		```bash
		# Forward Request
		SET FWD_GEOCODE:WALL_STREET_NY:5: '{"result": [...], "num_results": 5}' EX 15
		```

		* The request key is accessed during each API call to `Geocoder Edge` with a command similar to the following. Note, for un-cached results, this query will return no data.

		```bash
		GET FWD_GEOCODE:WALL_STREET_NY:5
		```

#### Asyncronous Geocode Requests

The asyncronous geocoding API makes requests through `Geocoder Edge` and uses `Redis Search` to handle address resolution. However, because this service is meant to provide precise updates on batch status, results are **NOT** cached in `Geocoder Web Cache`. In this section, I will detail the services and data structures unique to this API.


* `Batch Status Cache` - Treated as a status reference by `Batch Status Service`, this instance stores information about batches:

	* **BatchStatus** - A [HASH](https://redis.io/docs/data-types/hashes/) identified by `batch_uuid` (e.g. `60f011eb-3817-4b67-abed-af4a9aa50623`), containing fields for `status` (e.g. `BatchGeocodeStatus_ACCEPTED`, `BatchGeocodeStatus_SUCCESS`), `download_path`, and `update_time`. 

		* A new BatchStatus is created on a valid request to `https://gc.dmw2151.com/batch/`. The command to do so is similar to the following:

		```bash
		# Create Initial Batch Data - No Donwload Path Set on Initialization
		HSET ${BATCH_UUID} status "BatchGeocodeStatus_ACCEPTED" download_path "" update_time ${CURRENT_TIME}
		```

		* The BatchStatus is updated by a background process that recieves updates from `Event Bus`, these updates are sent by `Async Worker` and can indicate a request has finished, failed validation, been canceled, etc.

		```bash
		# If a `success` message is recieved from `Event Bus` -> Set Success
		HSET ${BATCH_UUID} status "BatchGeocodeStatus_SUCCESS" download_path ${A_LONG_SIGNED_URL} update_time ${CURRENT_TIME}
		```

		* The BatchStatus is accessed on a request to `https://gc.dmw2151.com/batch/${BATCH_UUID}` with a request like the below:

		```bash
		HMGET ${BATCH_UUID} status download_path update_time
		```

* `Event Bus` - Used for Pub/Sub - messages are sent between `Batch Status Service` and `Async Worker`. In practice, this instance maintains two channels.

	* **batch.creates** - A channel that `Batch Status Service` publishes on and `Async Worker` subscribes to. This channel contains the `batch_uuid`s of new batches.

		* A new message is created after `Batch Status Service` has a new file available for `Async Worker` to pick up. This channel only communicates the UUID of the new create, the command used to publish is similar to the following:

		```bash
		PUBLISH batch.creates ${BATCH_UUID}
		```

		* On initialization, `Async Worker` runs the following command to access future messages

		```bash
		SUBSCRIBE batch.creates
		```

	* **batch.status** - A channel that `Async Worker` publishes on and `Batch Status Service` subscribes to. This channel sends messages with the same schema as BatchStatus (as described in the `Batch Status Cache` section). However, instead of sending a hash, `Async Worker` sends a protobuf  representation of the BatchStatus object.

		*  `Async Worker` sends a message on this channel following any meaningful event in the batch geocoding process. 

		```bash
		PUBLISH batch.status ${A_PROTO_REPRESENTATION_OF_BATCHSTATUS}
		```

		* On initialization, `Batch Status Service` runs the following command to access future messages

		```bash
		SUBSCRIBE batch.status
		```

### Performance Benchmarks




## Running Locally

[Make sure you test this with a fresh clone of your repo, these instructions will be used to judge your app.]


### Prerequisites

- docker -> `Docker version 20.10.14, build a224086`
- docker-compose -> `docker-compose version 1.29.2`
- go -> `go version go1.18.5 darwin/amd64`


### Local Installation

[Insert instructions for local installation]


## Deployment

This application does not provide a quick deploy option, please refer to the local installation section for discussion on how to test the app


## TODO

There are some weaknesses in this service right now, the following would improve user experience, performance, etc.

* Set memory limits

* Retries

* Backpressure handling 

* HA redis

* Enums

* Open API Spec
