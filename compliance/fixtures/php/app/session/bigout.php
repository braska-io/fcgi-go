<?php
// Emits `bytes` of output followed by a marker, optionally flushing every
// `flush_every` bytes and optionally announcing Content-Length.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/bigout');

$bytes = (int) ($q['bytes'] ?? 1048576);
$flushEvery = (int) ($q['flush_every'] ?? 0);
$marker = sprintf('END bytes=%d', $bytes);
$fill = $bytes - strlen($marker);
if (($q['cl'] ?? '0') === '1') {
    header('Content-Length: ' . $bytes);
}
header('Content-Type: text/plain');
$block = str_repeat('0123456789abcdef', 4096); // 64 KiB
$step = $flushEvery > 0 ? $flushEvery : strlen($block);
for ($done = 0; $done < $fill;) {
    $n = min($step, $fill - $done);
    echo substr(str_repeat($block, intdiv($n, strlen($block)) + 1), 0, $n);
    $done += $n;
    if ($flushEvery > 0) {
        flush();
    }
}
echo $marker;
