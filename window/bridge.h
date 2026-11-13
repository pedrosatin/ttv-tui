#ifndef TTV_TUI_WINDOW_BRIDGE_H
#define TTV_TUI_WINDOW_BRIDGE_H

#ifndef GL_GLEXT_PROTOTYPES
#ifndef _WIN32
#define GL_GLEXT_PROTOTYPES 1
#endif
#endif
#include <SDL3/SDL.h>
#include <SDL3/SDL_opengl.h>
#include <mpv/client.h>
#include <mpv/render_gl.h>
#include <stdlib.h>

void init_gl_extensions(void);

void reg_events(void);
Uint32 ev_mpv(void);
Uint32 ev_tui(void);
Uint32 ev_ctl(void);
Uint32 ev_input(void);
void inject_input(const void *p, int n);
void *input_data(SDL_Event *e);
int input_len(SDL_Event *e);
void push_ev(Uint32 t);
void push_quit(void);
void inject_key(SDL_Keycode k, SDL_Keymod m);
Uint32 e_type(SDL_Event *e);
SDL_Keycode e_key(SDL_Event *e);
SDL_Keymod e_mod(SDL_Event *e);
const char *e_text(SDL_Event *e);
SDL_Window *make_window(const char *title);
mpv_render_context *make_rc(mpv_handle *m, SDL_Window *win);
int rc_has_frame(mpv_render_context *rc);
void rc_render(mpv_render_context *rc, int w, int h);
void rc_free(mpv_render_context *rc);
void load_fd(mpv_handle *m, const char *url);
void set_property(mpv_handle *m, const char *name, const char *value);
const char *gl_str(GLenum name);
int gl_max_tex(void);
int gl_error(void);
GLuint make_tex(void);
void free_tex(GLuint t);
void tex_alloc(GLuint t, int w, int h, int bgra);
void tex_rect(GLuint t, int x, int y, int w, int h, int stride_px, const void *p, int bgra);
void clear_to(int W, int H, float r, float g, float b);
void panel_begin(int W, int H);
void panel_tile(GLuint t, int x, int y, int w, int h, float u, float v);
void panel_end(void);
void fb_pixel(int x, int y, unsigned char *rgba);

#endif
