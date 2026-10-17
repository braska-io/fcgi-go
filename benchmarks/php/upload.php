<?php
// A multipart upload as a browser sends it: PHP writes the file to its temp
// directory, and the script hashes it.
$f = $_FILES['file'] ?? null;
if ($f === null || $f['error'] !== UPLOAD_ERR_OK) {
    http_response_code(400);
    echo "no file\n";
    return;
}
echo $f['size'], ' ', hash_file('sha256', $f['tmp_name']), "\n";
