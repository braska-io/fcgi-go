<?php
// Observer for scenarios: resets and reads the state other scripts leave.
//   op=reset&k=K               deletes rows and marks of K, body "reset"
//   op=count&k=K[&db=persist]  committed ev rows of K, decimal
//   op=marks&k=K               marks of K in write order, comma-separated
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/obs');
$k = $q['k'] ?? '';

switch ($q['op'] ?? '') {
case 'reset':
    foreach (['app.db', 'persist.db'] as $file) {
        app_db($file)->prepare('DELETE FROM ev WHERE k = ?')->execute([$k]);
    }
    marks_db()->prepare('DELETE FROM mark WHERE k = ?')->execute([$k]);
    echo 'reset';
    break;
case 'count':
    $file = ($q['db'] ?? '') === 'persist' ? 'persist.db' : 'app.db';
    $st = app_db($file)->prepare('SELECT COUNT(*) FROM ev WHERE k = ?');
    $st->execute([$k]);
    echo (string) $st->fetchColumn();
    break;
case 'marks':
    echo implode(',', marks($k));
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
