#!/usr/bin/env bash

file_exist=0
if [[ -f "sleep_and_check.txt" ]]; then
  file_exist=1
fi

if [[ $file_exist == "0" ]]; then
  sleep 5

  if  [[ -f "sleep_and_check.txt" ]]; then
    echo "File unexpectedly appeared" > sleep_and_check_fail.txt
    echo "File unexpectedly appeared"
    exit 1
  fi

  echo "file_contents" > sleep_and_check.txt
fi
