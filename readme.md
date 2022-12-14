# Geocoding with Redis

A geocoder is a service for matching addresses to geographic locations and the entities containing those addresses. Geocoders use both geospatial queries and full text search to resolve incomplete addresses to addresses and locations from a validated. This repo builds a geocoder using Redis Search and PubSub to provide both a synchronous and asynchronous geocoding services. I go into detail about the implementation of this application in this [walkthrough video](https://youtu.be/E2SqCKP4cvw).

## Application Description

| Figure 1.0 Synchronous Geocoding Architecture |
|-------------------------------------|
| ![arch](./misc/docs/_arch_sync.png)|

- The synchronous geocoding API allows a user to submit a query address or location and receive a list of scored, potentially matching addresses. See examples below.

```bash
# sample forward query :: address -> (address, coordinates)
curl -XPOST https://gc.dmw2151.com/geocode/ \
-d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "ATLANTIC AVE BROOKLYN"}' 

{
  "result": [
    {
      "address": {
        "location": {
          "latitude": 40.676468,
          "longitude": -73.909355
        },
        "id": "address:5185505" 
      },
      "normed_confidence": 1,
      "full_street_address": "2111 ATLANTIC AVE BROOKLYN 11233"
    }
  ],
  "num_results": 1
}
```

```bash
# sample reverse query :: coordinates -> (address, coordinates)
curl -XPOST https://gc.dmw2151.com/geocode/ \
-d '{"method": "REV_NEAREST", "max_results": 1, "query_lat": 40.677, "query_lng": -73.932 }'

{
  "result": [
    {
      "address": {
        "id": "address:9210336",
        "location": {
          "latitude": 40.675762,
          "longitude": -73.932205
        },
        "composite_street_address": "1682 DEAN ST BROOKLYN NEW YORK 11213"
      },
      "normed_confidence": 1
    }
  ],
  "num_results": 1
}
```

| Figure 1.1 Asynchronous Geocoding Architecture |
|-------------------------------------|
| ![arch](./misc/docs/_arch_full.jpg)|

- The asynchronous geocoding API allows a user to submit a batch of locations or addresses for geocoding. A request to this endpoint will kick-off a series of backend processes that asynchronously resolve all requested addresses. Moments later, the entire batch will be available to download via a signed URL.

  - `/batch/` - Handles the submission for new batch datasets. See example below, you may also find some sample batch endpoint inputs in `./misc/benchmarks`

    ```bash
    # Request - creates a new batch w. three addresses
    curl -XPOST https://gc.dmw2151.com/batch/ -d '{ 
            "method": "FWD_FUZZY", 
            "query_addr": [
                    "ATLANTIC AVE BROOKLYN",
                    "WALL STREET MANHATTAN",
                    "509 MAIN ST",
            ]
        }' 

    # Response - acknowledges batch creation and gives uuid for status updates
    {
        "id": "60f011eb-3817-4b67-abed-af4a9aa50623",
        "status": "ACCEPTED",
        "update_time": {
            "seconds": 1661575183,
            "nanos": 391420781
        }
    }
    ```

  - `/batch/${BATCH_UUID}` returns the status of the batch. If the batch is completed, the body will include a signed URL that can be used to download or share the results. In the example below, visiting `https://gcaas-data-storage.nyc3.digitaloceanspaces.com/datasets/${A_UNIQ_SIGNING_STRING}` would resolve to your specific, geocoded dataset.

    ```bash
    # Request - using the `id` from the create request, check the status of the request
    curl -XGET https://gc.dmw2151.com/batch/60f011eb-3817-4b67-abed-af4a9aa50623

    # Response - contains the `id`, the batch status (accepted, rejected, in_queue, succeeded, failed, etc...) and a download URL
    {
        "id": "60f011eb-3817-4b67-abed-af4a9aa50623",
        "status": "SUCCESS", 
        "update_time": {
            "seconds": 1661575185,
            "nanos": 391420781
        }
        "download_path": "https://gcaas-data-storage.nyc3.digitaloceanspaces.com/datasets/${A_UNIQ_SIGNING_STRING}"
    }
    ```

-------------

### How Data is Stored and Accessed

#### Synchronous Geocode Requests

- `Redis Search` - Stores validated addresses for our Geocoder, e.g. the superset of all possible results.

  - **Address** - A hash identified by `addressId` (e.g. `address:c24cf11b-79fb-4f78-a76b-7532c58a85ca`), containing fields for `composite_street_address` (e.g. `23 Wall Street, New York, NY 10005`) and `location` (e.g. `(40.706005, -74.008827)`).

    - Each address is stored during the initial data ingestion stage (see: `Management Service`) with a command similar to the following.

    ```bash
    HSET address:${ADDRESSID} location "(40.706005, -74.008827)" composite_street_addr "23 Wall Street, New York, NY 10005"
    ```

  - **Index** - An FT.Index of all `address:*` hashes.

    - The index is created on server initialization with the following command.

        ```bash
        FT.CREATE addr-idx ON HASH PREFIX 1 "address:" NOHL NOOFFSETS LANGUAGE "english" SCHEMA location GEO composite_street_address TEXT SORTABLE
        ```

    - The `Geocoder GRPC Service` accesses the index (and addresses) on each API call. When `Geocoder GRPC Service` receives a request from `Geocoder Edge`, the HTTP request parameters populate a search similar to the following.

        ```bash
        # Forward Geocode Request :: Address -> Fuzzy Match -> (Address, Location)
        FT.SEARCH addr-idx "@composite_street_address:${REQUEST_ADDR}" WITHSCORES LANGUAGE "english" SCORER TFIDF.DOCNORM LIMIT 0 ${REQUEST_MAX_RESULTS}

        # Reverse Geocode Request :: Point -> Geo Query -> (Address, Location)
        FT.SEARCH addr-idx "@location:[${REQUEST_LAT} ${REQUEST_LNG} 64 m]" WITHSCORES LIMIT 0 ${REQUEST_MAX_RESULTS}
        ```

- `Geocoder Web Cache` - Temporarily stores responses from `Redis Searh`. Prevents duplicate requests from hitting `Redis Search` in a short window.

  - **Request Key** - A deterministically-generated request key is stored as a string key. In practice, the key is simply a concatenation of the request parameters (e.g. `FWD_GEOCODE:WALL_STREET_NY:5`). The value of the key is the string representation of the query response. 

    - The request key is set following each successful API call to `Geocoder Edge` with a command similar to the following.

        ```bash
        # Forward Request
        SET FWD_GEOCODE:WALL_STREET_NY:5: '{"result": [...], "num_results": 5}' EX 15
        ```

    - The request key is accessed during each API call to `Geocoder Edge` with a command similar to the following. Note, for un-cached results, this query will return no data.

        ```bash
        GET FWD_GEOCODE:WALL_STREET_NY:5
        ```

#### Asynchronous Geocode Requests

The asynchronous geocoding API makes requests through `Geocoder Edge` and uses `Redis Search` to handle address resolution. However, it also contains services and data structures unique to this API.

- `Batch Status Cache` - Treated as a status reference by `Batch Status Service`, this instance stores information about batches:

  - **BatchStatus** - A hash identified by `batch_uuid` (e.g. `60f011eb-3817-4b67-abed-af4a9aa50623`), containing fields for `status` (e.g. `BatchGeocodeStatus_ACCEPTED`, `BatchGeocodeStatus_SUCCESS`), `download_path`, and `update_time`. 

    - A new BatchStatus is created on request to `https://gc.dmw2151.com/batch/`. The command to do so is similar to the following:

        ```bash
        # Create Initial Batch Data - No Download Path Set on Initialization
        HSET ${BATCH_UUID} status "BatchGeocodeStatus_ACCEPTED" download_path "" update_time ${CURRENT_TIME}
        ```

    - The BatchStatus is updated by a background process that receives updates from `Event Bus`, these updates are sent by `Async Worker` and can indicate a request has finished, failed validation, been canceled, etc.

        ```bash
        # If a `success` message is received from `Event Bus` -> Set Success
        HSET ${BATCH_UUID} status "BatchGeocodeStatus_SUCCESS" download_path ${A_LONG_SIGNED_URL} update_time ${CURRENT_TIME}
        ```

    - The BatchStatus is accessed on a request to `https://gc.dmw2151.com/batch/${BATCH_UUID}` with a request like the below:

        ```bash
        HMGET ${BATCH_UUID} status download_path update_time
        ```

- `Event Bus` - Used for pub/sub - messages are sent between `Batch Status Service` and `Async Worker`. In practice, this instance maintains two channels.
  
  - **batch.creates** - A channel that `Batch Status Service` publishes on and `Async Worker` subscribes to. This channel contains the `batch_uuid`s of new batches.

    - A new message is created after `Batch Status Service` has a new file available for `Async Worker` to pick up. This channel only communicates the UUID of the new create, the command used to publish is similar to the following:

    ```bash
    PUBLISH batch.creates ${BATCH_UUID}
    ```

    - On initialization, `Async Worker` runs the following command to access future messages

    ```bash
    SUBSCRIBE batch.creates
    ```

  - **batch.status** - A channel that `Async Worker` publishes on and `Batch Status Service` subscribes to. This channel sends messages with the same schema as BatchStatus (as described in the `Batch Status Cache` section). However, instead of sending a hash, `Async Worker` sends a protobuf representation of the BatchStatus object.

    -`Async Worker` sends a message on this channel following any meaningful event in the batch geocoding process. 

    ```bash
    PUBLISH batch.status ${A_PROTO_REPRESENTATION_OF_BATCHSTATUS}
    ```

    - On initialization, `Batch Status Service` runs the following command to access future messages

    ```bash
    SUBSCRIBE batch.status
    ```

### Performance Benchmarks

This is a new application, so there are no prior benchmarks without Redis. I ran the following quick tests against the synchronous endpoint, `https://gc.dmw2151.com/geocode/` to get a sense for performance. With low load on the system, the request resolves almost immediately (\~80ms), most of which is time in transit. 

```bash
# see: https://stackoverflow.com/a/22625150 for timing format
curl -w "@timing-fmt.txt" -o /dev/null -s https://gc.dmw2151.com/geocode \
    -d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "ATLANTIC AVE BROOKLYN"}'  

       timmelookup:  0.006658s
      time_connect:  0.028724s
   time_appconnect:  0.056905s
  time_pretransfer:  0.056950s
     time_redirect:  0.000000s
time_starttransfer:  0.080518s
                     ----------
        time_total:  0.080624s
```

I replicated this test using both cached requests (avg. \~70ms) and the health endpoint (avg. \~65ms). In general, the geocoding is *easy* for redis, and while the `Geocoder Web Cache` can help a bit, the "pain" in the system isn't so much load on the server (at this level of load) as it is the latency from a local machine to the geocoding server.

To simulate a higher load situation, I created a list of sample addresses to request for forward geocoding. I sent these over the wire with eight parallel processes. In this test I found that the API was able to handle this volume OK, but there's still some work to be done to improve performance here, I have not examined the traces to determine where this latency is, though Redis insights could help with that.

```bash
time (cat ./misc/benchmarks/generate-mock-load.txt |\
    xargs -P 8 -I % curl -o /dev/null -s https://gc.dmw2151.com/geocode/ -d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "%I"}')

# (21.875 seconds * 8 clients ) / 1000 requests -> ~175ms / request -> SHARP drop :(
15.32s user 9.30s system 112% cpu 21.875 total
```

Despite the last test, I would strongly discourage this usage of the synchronous API. It is meant for one off requests, the asynchronous service delivers a better product experience for users who want to geocode hundreds or thoudsands of locations. As further indication of this fact, I created a larger test file - containing 2,500 addresses and sent it to the batch server. Using the batch endpoint, I was able to geocode \~250 search requests/sec, a nice improvement over the sync API.


```bash
# init request at 1661735377.681408477
curl -s -XPOST https://gc.dmw2151.com/batch/ -d @$(pwd)/misc/benchmarks/fwd-batch-test-large.json

{
  "id": "9ff73be0-7022-437a-822e-8a7cdb6b8ea0",
  "status": "ACCEPTED",
  "update_time" :{
    "seconds":1661735377,
    "nanos":681408477
  }
}

# request shows status (4) `finished` 10 seconds later -> 1661735387.92403820
curl -s -XGET https://gc.dmw2151.com/batch/9ff73be0-7022-437a-822e-8a7cdb6b8ea0

{
  "id": "9ff73be0-7022-437a-822e-8a7cdb6b8ea0",
  "status": "SUCCESS",
  "download_path": "https://gcaas-data-storage.nyc3....",
  "update_time": {
    "seconds": 1661735387,
    "nanos": 92403820
  }
}
```


| Figure 2.0 Batch Endpoint Profiling |
|-------------------------------------|
| ![arch](./misc/docs/insights_batch_prof.png)|


## Running Locally

### Prerequisites

These instructions were tested on a machine with the following software. Any modern MacOS (M1 or X86) or other Unix based machine should be able to follow these instructions without issue.

- docker -> `Docker version 20.10.14, build a224086`
- docker-compose -> `docker-compose version 1.29.2`
- docker-desktop -> `4.11.1 (84025)`
- go -> `go version go1.18.5 darwin/amd64`
- kernel info -> `21.5.0 Darwin Kernel Version 21.5.0: Tue Apr 26 21:08:22 PDT 2022; root:xnu-8020.121.3~4/RELEASE_X86_64 x86_64`

### Local Installation

#### Section 1 - Deploying Services

Local installation does not involve deploying any paid resources to a cloud or accessing any resources from `gc.dmw2151.com`. However, it does require building and pulling all containers used in the project. I've built very lightweight service images, but machines with <4GB of RAM to allocate to Docker may struggle a bit on build. Change directories to `./deploy-development` and run `ENVIRONMENT="LOCAL" docker-compose up`. On the first run, this will build all containers associated with the project (*Estimated Time: 2-4 minutes*).

```bash
# Result of `docker ps`
CONTAINER ID   IMAGE                               COMMAND                  CREATED          STATUS          PORTS                      NAMES                       
0bb1faf8ddc4   deploy-development_gcaas-edge       "/cmd/edge/edge ' --???"   8 seconds ago    Up 7 seconds    0.0.0.0:2151->2151/tcp     deploy-development_gcaas-edge_1
b31daee0688f   redislabs/redisinsight:latest       "bash ./docker-entry???"   9 seconds ago    Up 8 seconds    0.0.0.0:8001->8001/tcp     deploy-development_insight_1                                                                   
bf4ed04049c3   deploy-development_gcaas-geocoder   "/cmd/geocoder/geoco???"   9 seconds ago    Up 8 seconds    0.0.0.0:50051->50051/tcp   deploy-development_gcaas-geocoder_1
3e35bcfaf658   deploy-development_gcaas-mgmt       "/cmd/mgmt/mgmt ' --???"   9 seconds ago    Up 8 seconds    0.0.0.0:50052->50052/tcp   deploy-development_gcaas-mgmt_1                                                                
3d118f6c80c3   deploy-development_gcaas-batch      "/cmd/batch/batch ' ???"   7 minutes ago    Up 7 minutes    0.0.0.0:50053->50053/tcp   deploy-development_gcaas-batch_1
5350f9bee4e6   deploy-development_gcaas-worker     "/cmd/worker/worker ???"   7 minutes ago    Up 7 minutes                               deploy-development_gcaas-worker_1
172dbac9549f   redislabs/redismod:latest           "redis-server --load???"   10 seconds ago   Up 10 seconds   6379/tcp                   deploy-development_search_1
2839c5f4e537   redis:alpine3.16                    "docker-entrypoint.s???"   7 minutes ago    Up 7 minutes    6379/tcp                   deploy-development_edge-cache_1
3ce695bbf297   redis:alpine3.16                    "docker-entrypoint.s???"   7 minutes ago    Up 7 minutes    6379/tcp                   deploy-development_pubsub_1
4699cbf81461   redis:alpine3.16                    "docker-entrypoint.s???"   7 minutes ago    Up 7 minutes    6379/tcp                   deploy-development_batch-cache_1
```

Once the build has finished, you should be able to run `docker stats` and see each of the following containers' resource usage. You can also run `docker ps` to see the ports that are exposed from the application to our `localhost`. If you'd like to tail the logs while you send requests to the service, you can also run `docker compose logs --follow` (highly recommended).

To confirm that all is up and running, we can send a request to `http://localhost:2151/geocode/`. Because we haven't seeded our dataset with any data, we expect the request to simply echo back our query.

```bash
# Request - Test Request to Confirm Edge Service is Running - Should Return No Data, but show HTTP Status 200
curl -XPOST http://localhost:2151/geocode/ \
    -d '{"method": "FWD_FUZZY", "max_results": 1, "query_addr": "ATLANTIC AVE"}' 

# Response -> The server echos queries back to the user as part of a response 
# NOTE: this is omitted throughout the remainder of this document
{
    "query": {
        "Query": {
            "AddressQuery": "%ATLANTIC% %AVE%"
        }
    }
}
```

#### Section 2 - Seeding the Location Index

The local configuration for this application exposes a service called `GCAAS Management` on `:50051`. In production, this service is used by the developer (me) to manage the data available in the production. In this step, we'll seed the location index by sending data through `GCAAS Management`.

This application could accept any dataset of addresses provided they met the ingestion criteria. For the time being, we'll use a dataset I'll refer to as `NYC Addresses 1M`. This [dataset](https://www.google.com/url?sa=t&rct=j&q=&esrc=s&source=web&cd=&cad=rja&uact=8&ved=2ahUKEwjM3PeE7Of5AhVpKlkFHRkqCkMQFnoECBQQAQ&url=https%3A%2F%2Fdata.cityofnewyork.us%2FCity-Government%2FNYC-Address-Points%2Fg6pj-hd8k&usg=AOvVaw2XeK_R5WgxJoP6Fjq61GZ1) contains 967,000 address points from New York City.

To download the NYC address dataset, change directories to `./misc/data-processing` and run `bash ./download-nyc.sh`. This script downloads and processes the raw NYC address data into a format that `GCAAS Mangement` accepts. (*Estimated Time: 2 - 3 minutes*)

You can inspect the first few rows of the cleaned dataset with, `head -n 10 ./_data/prepared_nyc.csv`.

```csv
ADDRESS_ID,the_geom,H_NO FULL_STREE BOROCODE NEW YORK ZIPCODE                                                                               
3066687,POINT (-73.94890840262882 40.681024605257534),54 MACON ST BROOKLYN NEW YORK 11216                                                                                  
3064205,POINT (-73.94867469809614 40.6862985441319),438 GATES AVE BROOKLYN NEW YORK 11216
3063204,POINT (-73.95302508854085 40.6880523616944),442 GREENE AVE BROOKLYN NEW YORK 11216
3065757,POINT (-73.94380067421258 40.68347876343482),411 TOMPKINS AVE BROOKLYN NEW YORK 11221
3066531,POINT (-73.9422673814714 40.682519773924874),290 HALSEY ST BROOKLYN NEW YORK 11216
3054846,POINT (-73.9386413566232 40.68971323068642),742 GREENE AVE BROOKLYN NEW YORK 11221
3060301,POINT (-73.94438289946332 40.699143336060835),176 THROOP AVE BROOKLYN NEW YORK 11206
3060994,POINT (-73.9445761259967 40.69528388494299),185 VERNON AVE BROOKLYN NEW YORK 11206
3062642,POINT (-73.95194963018604 40.68967344199405),574 LAFAYETTE AVE BROOKLYN NEW YORK 11205
```

I've provided a short Go script that interacts with the management endpoint and can be used to populate the location index with the NYC dataset. If you do not have Go installed, please run through the [installation guide](https://go.dev/doc/install) for your OS and proceed to the next steps(*Estimated Time: 10 - 15 minutes*).

Change directories into `./geocoder-svc/cmd/seed-address-dataset/` and run the command below. (*Estimated Time: <1 minute*).

```bash
# command start
go run . --rpc-server localhost \
    --rpc-server-port 50052 \
    --file ./../../../misc/data-processing/_data/prepared_nyc.csv

# expected command output
INFO[0028] /geocoder.Management/InsertorReplaceAddressData; success  insert.num_objects=967507 insert.success=true
```

Finally, we can retry the query that previously returned no results and see a matched address.

```bash
# request - retry original address query from section 1
curl -XPOST http://localhost:2151/geocode/ \
    -d '{"method": "FWD_FUZZY", "max_results": 1, "query_addr": "ATLANTIC AVE"}' 

# response - 
{
  "result": [
    {
      "address": {
        "id": "address:3043414",
        "location": {
          "latitude": 40.67734,
          "longitude": -73.932465
        }
      },
      "normed_confidence": 1,
      "composite_street_address": "1748 ATLANTIC AVE BROOKLYN NEW YORK 11213"
    }
  ],
  "num_results": 1
}
```

#### Section 3 - Using Redis Insights

Now that we have a working service that's reading from the `Redis Search` instance, we should understand how its performing. The local configuration runs Redis Insights on `:8001`. You can visit the local insights instance on localhost [here](http://localhost:8001). You can connect to profile the search instance using the parameters shown below:

![insights](./misc/docs/insights.png)

#### Section 4 - Calling The Asynchronous API

Now that the API is running and data is loaded in, calling the async API should require no additional configuration. Try sending the following request to the API's `/batch/` endpoint from the root of this project.

```bash
# request - uses a batch of 100 addresses
 curl -XPOST http://localhost:2151/batch/ \
    -d @$(pwd)/misc/benchmarks/fwd-batch-test.json

# response - api responds with acknowledgement of new batch
{
    "id": "dd709eea-59fa-4fac-b7e8-886d5c44c97f",
    "status": "ACCEPTED",
    "update_time": {
        "seconds": 1661654679,
        "nanos": 792647238
    }
}
```

The response above should return immediately (\~100ms) acknowledging the batch was created. You should then be able to call the `/batch/${JUST_CREATED_UUID}` endpoint and get the batch status.

```bash
# request - check on newly created batch
curl -XGET http://localhost:2151/batch/dd709eea-59fa-4fac-b7e8-886d5c44c97f

# response - status of newly created batch (for 100 addresses, this should take <3s to resolve)
{
    "id":"dd709eea-59fa-4fac-b7e8-886d5c44c97f",
    "status": "SUCCESS",
    "download_path": "dd709eea-59fa-4fac-b7e8-886d5c44c97f-results.json",
    "update_time":{
        "seconds":1661654681,
        "nanos":400917319
    }
}
```

In the production deployment, the `download_path` of the response would be a signed URL to an object on DigitalOcean. In the local configuration, you should be able to see the file created on your host at `./deploy-development/tmp/${PATH}`. Feel free to try your own batches, or test batches provided in the `benchmarks` folder.

## Deployment

This application does not provide a quick deploy option, please refer to the local installation section for discussion on how to test the app.

## TO-DO

There are some weaknesses in this service right now, the following would improve user experience, performance, etc.

- Set Memory Limits - In the current configuration, different instances have different resource requirements, the application's deployment could be safer if these were scoped and defined.

- Back-pressure / Retries - In the current application, there's minimal logic for retries, timed back-offs, etc. In short, the properties you'd write into a resilient distributed system aren't present.

- High Availability - The application is not deployed for HA. Because we run on a single node, we've got every service sharing the same underlying pool of resources, there is no redundancy, etc. A proper deployment of this service would involve migrating to either DigitalOcean's K8s offering, or a self-hosted Consul + Nomad deployment.

- Open API Specification - The API behavior is not well documented to the public. [Open API](https://swagger.io/specification/) is a specification that makes documenting services easier, but I haven't yet written this spec.

- Datasets - The service is designed to handle *any* dataset with id, location, and address. There are quite a few national datasets available that could be interesting, e.g. [NAD](https://www.transportation.gov/gis/national-address-database/national-address-database-nad-disclaimer).
