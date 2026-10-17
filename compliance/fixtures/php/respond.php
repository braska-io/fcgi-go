<?php
require __DIR__ . '/_fixture.php';
$q = fixture('respond');
if (isset($q['sleep_ms'])) {
    usleep((int) $q['sleep_ms'] * 1000);
}
if (isset($q['status'])) {
    http_response_code((int) $q['status']);
}
if (isset($q['raw_status'])) {
    header('Status: ' . $q['raw_status']);
}
foreach ((array) ($q['header'] ?? []) as $h) {
    header($h, false);
}
if (isset($q['body'])) {
    echo $q['body'];
}
if (isset($q['body_size'])) {
    echo pattern((int) $q['body_size']);
}
if (isset($q['stderr'])) {
    error_log($q['stderr']);
}
if (($q['flush'] ?? '') === '1') {
    flush();
}
