<?php
// Two calls show that PHP removes an unmoved upload after the request:
// a POST records tmp_name under ?key=, a later GET with the same key reports
// whether that file still exists.
require __DIR__ . '/_emit.php';
$q = fixture('app/request/tmpfile');
$key = preg_replace('/[^a-z0-9-]/', '', $q['key'] ?? 'default');
$record = (getenv('FIXTURE_STATE') ?: '/state') . '/tmpfile-' . $key;
if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    $tmp = $_FILES['f']['tmp_name'] ?? '';
    file_put_contents($record, $tmp);
    emit(['recorded' => $tmp !== '', 'exists_during_request' => $tmp !== '' && file_exists($tmp)]);
    return;
}
$tmp = is_file($record) ? file_get_contents($record) : '';
@unlink($record);
emit(['recorded' => $tmp !== '', 'exists_after_request' => $tmp !== '' && file_exists($tmp)]);
