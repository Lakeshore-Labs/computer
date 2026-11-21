/* _host: a Python script's one call to the program that runs the interpreter. The host exports two functions in
   the import module "computer": call(ptr, n) takes a JSON message from this module's memory and returns the
   length of the reply, and read(ptr) copies the reply into it. */
#include "Python.h"

__attribute__((import_module("computer"), import_name("call")))
extern uint32_t host_call(const char *ptr, uint32_t n);
__attribute__((import_module("computer"), import_name("read")))
extern void host_read(char *ptr);

static PyObject *
host_call_py(PyObject *self, PyObject *arg)
{
    Py_buffer message;
    if (PyObject_GetBuffer(arg, &message, PyBUF_SIMPLE) < 0) {
        return NULL;
    }
    uint32_t n = host_call(message.buf, (uint32_t)message.len);
    PyBuffer_Release(&message);
    PyObject *reply = PyBytes_FromStringAndSize(NULL, n);
    if (reply != NULL && n > 0) {
        host_read(PyBytes_AS_STRING(reply));
    }
    return reply;
}

static PyMethodDef methods[] = {
    {"call", host_call_py, METH_O, "call(message: bytes) -> bytes. One JSON message to the host and its JSON reply."},
    {NULL, NULL, 0, NULL},
};

static struct PyModuleDef module = {PyModuleDef_HEAD_INIT, "_host", "The host this script runs in.", -1, methods};

PyMODINIT_FUNC
PyInit__host(void)
{
    return PyModule_Create(&module);
}
