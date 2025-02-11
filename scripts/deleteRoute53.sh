#!/bin/bash

if [[ -z $1 || -z $2 || -z ${3} ]]; then
  echo "Usage: ZONEID PREFIX DOMAIN"
  exit 1
fi

HOSTED_ZONE_ID=$1
PREFIX="$2-"
PREFIX_CNAME="$2-cname-"
DOMAIN=$3

DOMAIN_DOT="${DOMAIN}."
PREFIX_DOMAIN="${PREFIX}${DOMAIN_DOT}"
PREFIX_CNAME_DOMAIN="${PREFIX_CNAME}${DOMAIN_DOT}"

# Function to delete a record if it exists
delete_record() {
  local RECORD_NAME=$1
  local RECORD_TYPE=$2

  # Fetch the record
  RECORD_JSON=$(aws route53 list-resource-record-sets --hosted-zone-id "$HOSTED_ZONE_ID" --query "ResourceRecordSets[?Name == '$RECORD_NAME' && Type == '$RECORD_TYPE'] | [0]" --output json)

  # If a record is found, delete it
  if [[ "$RECORD_JSON" != "null" && "$RECORD_JSON" != "[]" ]]; then
    echo "Deleting $RECORD_TYPE record: $RECORD_NAME"
    aws route53 change-resource-record-sets --hosted-zone-id "$HOSTED_ZONE_ID" --change-batch "$(echo "$RECORD_JSON" | jq -c '{Changes: [{Action: "DELETE", ResourceRecordSet: {Name: .Name, Type: .Type, TTL: .TTL, ResourceRecords: .ResourceRecords}}]}')"
  else
    echo "No $RECORD_TYPE record found for: $RECORD_NAME"
  fi
}

echo "Checking and deleting records for: $DOMAIN_DOT"
# Check and delete CNAME record
delete_record "$DOMAIN_DOT" "CNAME"

# Check and delete TXT records with prefixes
delete_record "$PREFIX_DOMAIN" "TXT"
delete_record "$PREFIX_CNAME_DOMAIN" "TXT"

echo "Completed record deletions."
