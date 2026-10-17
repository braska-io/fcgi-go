<?php
// A request that waits on I/O, such as a database or HTTP call, while the
// worker stays busy.
usleep(1000 * (int) ($_GET['ms'] ?? 20));
echo "ok\n";
