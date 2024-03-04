#!/bin/bash

DBSTRING="host=$DBHOST user=$DBUSER password=$DBPASSWORD dbname=$DBNAME sslmode=disable"

goose postgres "$DBSTRING" up