<?php
// Emits n bytes, then tries to send a header, cookie or session cookie.
// With output_buffering=4096 the headers leave once the buffer overflows.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/late');

echo str_repeat('.', (int) ($q['n'] ?? 0));
switch ($q['case'] ?? 'header') {
    case 'header':
        @header('X-Late: 1');
        echo "\nsent=" . (headers_sent() ? 'true' : 'false');
        break;
    case 'setcookie':
        $r = @setcookie('late', '1');
        echo "\nsetcookie=" . ($r ? 'true' : 'false');
        break;
    case 'session':
        $r = @session_start();
        echo "\nsession_start=" . ($r ? 'true' : 'false');
        break;
    case 'ob':
        // A user buffer holds everything, so headers stay unsent regardless of size.
        // The ini buffer is level 1; this one nests above it.
        ob_start();
        echo str_repeat('.', 10000);
        @header('X-Late: 1');
        $sent = headers_sent();
        ob_end_flush();
        echo "\nsent=" . ($sent ? 'true' : 'false');
        break;
}
