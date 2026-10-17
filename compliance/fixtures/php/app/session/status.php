<?php
// Reports session_status() at each stage of a session's life.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/status');

function status_name(): string
{
    return match (session_status()) {
        PHP_SESSION_DISABLED => 'disabled',
        PHP_SESSION_NONE => 'none',
        PHP_SESSION_ACTIVE => 'active',
    };
}

$out = [status_name()];
switch ($q['case'] ?? 'write_close') {
    case 'write_close':
        session_start();
        $out[] = status_name();
        session_write_close();
        $out[] = status_name();
        break;
    case 'read_and_close':
        session_start(['read_and_close' => true]);
        $out[] = status_name();
        break;
    case 'name':
        $out[] = session_name();
        break;
}
echo implode(',', $out);
