#! /bin/bash

# Downloads NYC Address Data from the NYC Open Data Portal - For use w. Local Testing of Geocoder
# Webpage: https://data.cityofnewyork.us/City-Government/NYC-Address-Points/g6pj-hd8k

# Prepares the dataset to the format accepted by the /geocoder.Management/InsertOrReplaceData call

mkdir -p ./_data/

wget 'https://data.cityofnewyork.us/api/views/emzr-v3pi/rows.csv?accessType=DOWNLOAD' \
	-O ./_data/original_nyc.csv

cat ./_data/original_nyc.csv |\
	cut -d',' -f1,2,4,9,10,23 |\
	awk 'BEGIN  { FS = OFS = ","; } 
			$(4) == 1 { $(4) = "MANHATTAN"; } 
			$(4) == 2 { $(4) = "BRONX"; } 
			$(4) == 3 { $(4) = "BROOKLYN"; }  
			$(4) == 4 { $(4) = "QUEENS"; }  
			$(4) == 5 { $(4) = "STATEN ISLAND"; }  
		{ print; }'|\
	awk -F ',' '{ print $2 "," $1 "," $3 " " $6 " " $4 " NEW YORK " $5}' | tr -s ' ' > ./_data/prepared_nyc.csv
