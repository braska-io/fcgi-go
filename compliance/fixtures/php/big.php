<?php
require __DIR__ . '/_fixture.php';
$q = fixture('big');
$left = (int) ($q['bytes'] ?? 0);
while ($left > 0) {
    $n = min($left, 65536);
    echo pattern($n);
    $left -= $n;
}
