<?php
// header('Location: ...') and its interaction with an already-set status.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/redirect');

if (isset($q['pre'])) {
    http_response_code((int) $q['pre']);
}
$to = $q['to'] ?? '/target';
if (isset($q['code'])) {
    header('Location: ' . $to, true, (int) $q['code']);
} else {
    header('Location: ' . $to);
}
if (isset($q['post'])) {
    http_response_code((int) $q['post']);
}
