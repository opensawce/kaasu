# Load tests

`orders.js` drives order creation at 50 per second for a minute and fails the run if p95 is over 250 ms or more than 1% of requests fail.

Results are not published from a laptop run. Record the hardware with the numbers, and run on the target class (a Raspberry Pi-class box) before claiming any capacity.
