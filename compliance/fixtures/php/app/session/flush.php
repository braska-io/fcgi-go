<?php
// flush() and implicit flushing, observed through headers_sent().
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/flush');

switch ($q['case'] ?? 'flush') {
    case 'flush':
        echo 'a';
        flush();
        break;
    case 'ob_flush':
        echo 'a';
        ob_flush();
        flush();
        break;
    case 'implicit':
        ob_implicit_flush(true);
        while (ob_get_level() > 0) {
            ob_end_flush();
        }
        echo 'a';
        break;
}
@header('X-Late: 1');
echo "\nsent=" . (headers_sent() ? 'true' : 'false');
