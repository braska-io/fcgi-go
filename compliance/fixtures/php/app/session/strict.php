<?php
// Reports whether session_start() adopted the id the client sent.
require __DIR__ . '/../../_fixture.php';
fixture('app/session/strict');

$name = session_name();
$sent = $_COOKIE[$name] ?? $_GET[$name] ?? null;
session_start();
if ($sent === null) {
    echo 'no-id-sent';
} elseif (session_id() === $sent) {
    echo 'adopted';
} else {
    echo 'new';
}
