<?php
require __DIR__ . '/_fixture.php';
// error_log() without an error_log file goes to the SAPI logger, which
// php-fpm sends as FCGI_STDERR records.
$q = fixture('stderr');
$left = (int) ($q['bytes'] ?? 0);
$interleave = ($q['interleave'] ?? '') === '1';
while ($left > 0) {
    $n = min($left, 1024);
    error_log(str_repeat('e', $n));
    if ($interleave) {
        echo str_repeat('o', 1024);
        flush();
    }
    $left -= $n;
}
