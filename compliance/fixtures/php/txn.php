<?php
require __DIR__ . '/_fixture.php';
$q = fixture('txn');
ignore_user_abort(true); // the fixture decides about the abort itself

$db = new PDO('sqlite:' . getenv('FIXTURE_STATE') . '/state.db');
$db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
$db->exec('PRAGMA busy_timeout = 5000');
$db->exec('CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, n INTEGER NOT NULL)');
$k = $q['k'] ?? '';

switch ($q['op'] ?? '') {
case 'reset':
    $db->exec('DELETE FROM kv');
    echo 'reset';
    break;
case 'count':
    $st = $db->prepare('SELECT COUNT(*) FROM kv WHERE k = ?');
    $st->execute([$k]);
    echo (string) $st->fetchColumn();
    break;
case 'insert':
    $db->exec('BEGIN IMMEDIATE');
    $db->prepare('INSERT INTO kv (k, n) VALUES (?, 1)')->execute([$k]);
    usleep((int) ($q['sleep_ms'] ?? 0) * 1000);
    // ob_flush() pushes output_buffering's buffer to the SAPI; flush()
    // alone would write nothing and never notice a closed connection. Two
    // writes, because the first one after the peer closed can still succeed.
    echo 'insert';
    ob_flush();
    flush();
    usleep(50000);
    echo 'ed';
    ob_flush();
    flush();
    if (connection_aborted()) {
        $db->exec('ROLLBACK');
        exit;
    }
    $db->exec('COMMIT');
    break;
case 'insert_commit_crash':
    $db->exec('BEGIN');
    $db->prepare('INSERT INTO kv (k, n) VALUES (?, 1)')->execute([$k]);
    $db->exec('COMMIT');
    die_by('SEGV');
    break;
case 'insert_crash':
    $db->exec('BEGIN');
    $db->prepare('INSERT INTO kv (k, n) VALUES (?, 1)')->execute([$k]);
    die_by('SEGV');
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
