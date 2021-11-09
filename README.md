# malware-scanner

MalwareScanner is a tool for scanning skylinks for malware, CSAM, and other kinds of unwanted content.

The service exposes a REST endpoint for reporting skylinks and takes care internally to queue them up and scan them.
Once scanned, the skylinks are removed from the database and only their hashes are preserved, as to not keep any
pointers to illegal content.
