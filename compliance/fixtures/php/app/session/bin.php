<?php
// Byte-exact bodies that look like protocol framing.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/bin');

header('Content-Type: application/octet-stream');
switch ($q['case'] ?? 'ascii') {
    case 'ascii':
        for ($i = 0; $i < 128; $i++) {
            echo chr($i);
        }
        break;
    case 'nul':
        echo "\0\0\0";
        break;
    case 'header_like':
        echo "\r\n\r\nStatus: 500\r\nX-Injected: 1\r\n\r\n";
        break;
}
