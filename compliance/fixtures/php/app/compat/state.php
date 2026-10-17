<?php
// Observer for the compat scenarios.
//   op=reset&k=K  removes the marker of K, body "reset"
//   op=read&k=K   the marker's content, or "none"
require __DIR__ . '/_json.php';
$q = fixture('app/compat/state');
$marker = marker_path($q['k'] ?? '');

switch ($q['op'] ?? '') {
case 'reset':
    @unlink($marker);
    echo 'reset';
    break;
case 'read':
    clearstatcache();
    echo is_file($marker) ? file_get_contents($marker) : 'none';
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
