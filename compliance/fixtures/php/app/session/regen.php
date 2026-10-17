<?php
// Regenerates the session id and reports what changed and what was kept.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/regen');

session_start();
$old = session_id();
$ok = session_regenerate_id(($q['delete'] ?? '0') === '1');
$new = session_id();
clearstatcache();
printf(
    'ok=%d changed=%d old_file=%d user=%s',
    $ok,
    $old !== $new,
    file_exists(session_save_path() . '/sess_' . $old),
    $_SESSION['user'] ?? '(unset)'
);
