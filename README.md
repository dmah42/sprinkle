#Herd
Split into a few pieces.

command line 'shepherd' that multicasts for discovery, gathers statuses, requests running job.
service (grpc) 'sheep' that listen for discovery pings, and respond to rpc requests.
ui that multicasts for discovery and presents overview of the herd, and drill down into the sheep.
