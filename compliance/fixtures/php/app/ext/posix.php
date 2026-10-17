<?php
// Process identity; with op=record/distinct it shows which children served.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/posix');
header('Content-Type: text/plain');

$dir = getenv('FIXTURE_STATE') . '/ext-posix';
switch ($q['op'] ?? '') {
    case 'pid':
        echo var_export(posix_getpid() === getmypid(), true), ',', var_export(posix_getppid() !== posix_getpid(), true);
        break;
    case 'reset':
        foreach (glob($dir . '/*') ?: [] as $f) {
            unlink($f);
        }
        @mkdir($dir, 0777, true);
        echo 'reset';
        break;
    case 'record':
        // Sleeps so that parallel requests overlap; a child serves one at a time.
        @mkdir($dir, 0777, true);
        usleep((int) ($q['sleep_ms'] ?? 0) * 1000);
        touch($dir . '/' . posix_getpid());
        echo 'recorded';
        break;
    case 'distinct':
        echo count(glob($dir . '/*') ?: []);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
