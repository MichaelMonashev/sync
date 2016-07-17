# Goals

 - No bugs.
 - Keep API simple and stable.
 - Low CPU/memory consumption (code inlining, avoid memory allocation, avoid memory copy).
 - Simple, well-documented code. Suitable for easy porting to other languages.
 - No tricks (for compatibility with future Go versions).

## To Do list

High priority:

 - Documentation
 - 100% test covering
 - Benchmarks

Medium priority:

 - UnlockAll()
 - LockIfUnlocked()

Low priority:

 - RLock()
 - LockEach()
 - MTU, RTT background calculation
 - Choose best node based on MTU and RTT
 - Requests/response pipelining (packing many requests into 1 packet, parse packets contained many responses)
 - Requests/response Snappy compression to decrease number of packets
 - Requests/response checksum to avoid hardware errors
