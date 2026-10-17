<?php
// zlib.output_compression switched on at runtime, before any output.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/gz');

ini_set('zlib.output_compression', ($q['off'] ?? '0') === '1' ? '0' : '1');
header('Content-Type: text/plain');
echo str_repeat('compress me ', (int) ($q['n'] ?? 1000));
