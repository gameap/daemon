#!/usr/bin/env bash

trap ctrl_c INT

function ctrl_c() {
  echo "Stopping server..."
  exit 0
}

echo "Server starting..."
sleep 0.3
echo "Loading configuration..."

sleep 2
echo "Server started"

while [ true ]
do
   sleep 30
   date +%s
done
