<?php
// Writes "ok", pushes it past PHP, sleeps, and leaves a marker from a
// shutdown function, as cleanup code after a long request does.
//   k=K          marker key
//   sleep_ms=N   sleep after the flush (default 1000)
require __DIR__ . '/_json.php';
$q = fixture('app/compat/disconnect');
$marker = marker_path($q['k'] ?? '');
register_shutdown_function(function () use ($marker) {
    file_put_contents($marker, 'shutdown');
});
echo 'ok';
ob_flush();
flush();
usleep((int) ($q['sleep_ms'] ?? 1000) * 1000);
