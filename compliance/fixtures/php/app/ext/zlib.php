<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/zlib');

if (($q['op'] ?? '') === 'output_compression') {
    // Must be set before any output (php.net zlib.output_compression).
    ini_set('zlib.output_compression', '1');
    header('Content-Type: text/plain');
    echo str_repeat('compress me ', 200);
    return;
}

header('Content-Type: text/plain');
$data = str_repeat('fcgi ', 100);
switch ($q['op'] ?? '') {
    case 'gzip_roundtrip':
        $gz = gzencode($data);
        echo var_export(gzdecode($gz) === $data, true), ',', var_export(strlen($gz) < strlen($data), true);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
