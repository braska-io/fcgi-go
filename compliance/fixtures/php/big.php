<?php
require __DIR__ . '/_fixture.php';
$q = fixture('big');
$left = (int) ($q['bytes'] ?? 0);
// The fixture measures size, not time; a slow runner must not hit max_execution_time.
set_time_limit(0);
while ($left > 0) {
    $n = min($left, 65536);
    echo pattern($n);
    $left -= $n;
}
